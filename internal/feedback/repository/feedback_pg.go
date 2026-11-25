package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgStore struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) Store { return pgStore{db} }

// A group's size less one is the rows that point at its root.
const columns = `f.id, f.account_id, coalesce(f.mood, 0), coalesce(f.kind, ''), coalesce(f.body, ''), f.photos,
       f.device_model IS NOT NULL, coalesce(f.device_model, ''), coalesce(f.device_os, ''), coalesce(f.app_version, ''),
       coalesce(f.screen, ''), f.contact, f.source, coalesce(f.page_url, ''), f.status, coalesce(f.status_note, ''), coalesce(f.shipped_version, ''), f.status_at,
       coalesce(f.root_id, f.id), (SELECT count(*) FROM feedback g WHERE g.root_id = coalesce(f.root_id, f.id)),
       coalesce(f.reply_body, ''), f.replied_at, f.hearted, f.created_at`

func scan(row pgx.CollectableRow) (Feedback, error) { return scanWith(row) }

// scanWith scans columns into a Feedback, then the row's further columns
// into extra.
func scanWith(row pgx.CollectableRow, extra ...any) (Feedback, error) {
	var (
		f         Feedback
		hasDevice bool
		d         domain.Device
	)
	if err := row.Scan(append([]any{
		&f.ID, &f.AccountID, &f.Mood, &f.Kind, &f.Text, &f.Photos, &hasDevice, &d.Model, &d.OS, &d.AppVersion,
		&f.Screen, &f.Contact, &f.Source, &f.PageURL, &f.Status, &f.StatusNote, &f.ShippedVersion, &f.StatusAt, &f.RootID, &f.Others,
		&f.Reply, &f.RepliedAt, &f.Hearted, &f.CreatedAt,
	}, extra...)...); err != nil {
		return Feedback{}, fmt.Errorf("scan feedback: %w", err)
	}
	if hasDevice {
		f.Device = &d
	}
	return f, nil
}

// querier is a pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func read(ctx context.Context, q querier, where string, args pgx.StrictNamedArgs) ([]Feedback, error) {
	rows, err := q.Query(ctx, `SELECT `+columns+` FROM feedback f `+where, args)
	if err != nil {
		return nil, fmt.Errorf("select feedback: %w", err)
	}
	out, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("read feedback: %w", err)
	}
	return out, nil
}

// one reads the feedback id; domain.ErrNotFound for none.
func one(ctx context.Context, q querier, id uuid.UUID) (Feedback, error) {
	fs, err := read(ctx, q, `WHERE f.id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Feedback{}, err
	}
	if len(fs) == 0 {
		return Feedback{}, domain.ErrNotFound
	}
	return fs[0], nil
}

// orNull is s, or NULL for an empty s.
func orNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// memberOf is the member who acted, or NULL for the CLI.
func memberOf(a audit.Actor) any {
	if a.Kind != audit.ActorMember {
		return nil
	}
	return a.MemberID
}

// lockGroups serialises every write on groups.
func lockGroups(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('feedback:groups'))`); err != nil {
		return fmt.Errorf("lock groups: %w", err)
	}
	return nil
}

func (s pgStore) Insert(ctx context.Context, f Feedback, key uuid.UUID, since time.Time) (Feedback, bool, error) {
	var (
		out     Feedback
		existed bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// A share lock on the account: a send racing its deletion lands
		// before it, and the worker's Forget removes it, or sees the tombstone.
		living, err := postgres.LockLiving(ctx, tx, f.AccountID)
		if err != nil {
			return err
		}
		if !living {
			return accountdomain.ErrSessionRevoked
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('feedback:' || @account::text))`,
			pgx.StrictNamedArgs{"account": f.AccountID}); err != nil {
			return fmt.Errorf("lock the account's feedback: %w", err)
		}
		prior, err := read(ctx, tx, `WHERE f.account_id = @account AND f.request_key = @key`,
			pgx.StrictNamedArgs{"account": f.AccountID, "key": key})
		if err != nil {
			return err
		}
		if len(prior) > 0 {
			out, existed = prior[0], true
			return nil
		}
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM feedback WHERE account_id = @account AND created_at > @since`,
			pgx.StrictNamedArgs{"account": f.AccountID, "since": since}).Scan(&n); err != nil {
			return fmt.Errorf("count feedback: %w", err)
		}
		if n >= domain.DailyLimit {
			return domain.ErrTooMany
		}
		var model, os, version, mood any
		if d := f.Device; d != nil {
			model, os, version = d.Model, d.OS, orNull(d.AppVersion)
		}
		if f.Mood != 0 {
			mood = f.Mood
		}
		source := f.Source
		if source == "" {
			source = domain.SourceApp
		}
		photos := f.Photos
		if photos == nil {
			photos = []domain.Photo{}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO feedback (id, account_id, request_key, mood, kind, body, photos, device_model, device_os,
    app_version, screen, contact, source, page_url, status, status_at, hearted, created_at)
VALUES (@id, @account, @key, @mood, @kind, @body, @photos, @model, @os, @version, @screen, @contact, @source, @page, 'sent', @at, false, @at)`,
			pgx.StrictNamedArgs{
				"id": f.ID, "account": f.AccountID, "key": key, "mood": mood, "kind": orNull(string(f.Kind)), "body": orNull(f.Text),
				"photos": photos, "model": model, "os": os, "version": version, "screen": orNull(f.Screen), "contact": f.Contact,
				"source": string(source), "page": orNull(f.PageURL),
				"at": f.CreatedAt,
			}); err != nil {
			return fmt.Errorf("insert feedback: %w", err)
		}
		out, err = one(ctx, tx, f.ID)
		return err
	})
	if err != nil {
		return Feedback{}, false, fmt.Errorf("send feedback: %w", err)
	}
	return out, existed, nil
}

func (s pgStore) ByKey(ctx context.Context, account, key uuid.UUID) (Feedback, error) {
	fs, err := read(ctx, s.db, `WHERE f.account_id = @account AND f.request_key = @key`,
		pgx.StrictNamedArgs{"account": account, "key": key})
	if err != nil {
		return Feedback{}, err
	}
	if len(fs) == 0 {
		return Feedback{}, domain.ErrNotFound
	}
	return fs[0], nil
}

func (s pgStore) SentSince(ctx context.Context, account uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM feedback WHERE account_id = @account AND created_at > @since`,
		pgx.StrictNamedArgs{"account": account, "since": since}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count feedback: %w", err)
	}
	return n, nil
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Feedback, error) { return one(ctx, s.db, id) }

func (s pgStore) OfAccount(ctx context.Context, account uuid.UUID, limit int) ([]Feedback, error) {
	return read(ctx, s.db, `WHERE f.account_id = @account ORDER BY f.created_at DESC, f.id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "limit": limit})
}

func (s pgStore) Queue(ctx context.Context, status *domain.Status, kind *domain.Kind, p page.Request) ([]Feedback, error) {
	return read(ctx, s.db, `WHERE (@status::text IS NULL OR f.status = @status) AND (@kind::text IS NULL OR f.kind = @kind)
  AND f.id < @before ORDER BY f.id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"status": status, "kind": kind, "before": p.Before(), "limit": p.Fetch()})
}

func (s pgStore) Group(ctx context.Context, root uuid.UUID, limit int) ([]Feedback, error) {
	return read(ctx, s.db, `WHERE coalesce(f.root_id, f.id) = @root ORDER BY f.id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"root": root, "limit": limit})
}

func (s pgStore) Groups(ctx context.Context, kind *domain.Kind, limit int, now time.Time) ([]Group, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+`, g.size, g.moods, g.app, g.web, g.days FROM feedback f
CROSS JOIN LATERAL (
    SELECT count(*) AS size,
           ARRAY[count(*) FILTER (WHERE m.mood = 1), count(*) FILTER (WHERE m.mood = 2), count(*) FILTER (WHERE m.mood = 3),
                 count(*) FILTER (WHERE m.mood = 4), count(*) FILTER (WHERE m.mood = 5)] AS moods,
           count(*) FILTER (WHERE m.source = 'app') AS app, count(*) FILTER (WHERE m.source = 'web') AS web,
           ARRAY(SELECT count(*) FILTER (WHERE m2.created_at >= @now::timestamptz - (d + 1) * interval '1 day' AND m2.created_at < @now::timestamptz - d * interval '1 day')
                 FROM generate_series(6, 0, -1) AS d LEFT JOIN feedback m2 ON coalesce(m2.root_id, m2.id) = f.id
                 GROUP BY d ORDER BY d DESC) AS days
    FROM feedback m WHERE coalesce(m.root_id, m.id) = f.id
) g
WHERE f.root_id IS NULL AND (@kind::text IS NULL OR f.kind = @kind)
ORDER BY g.size DESC, f.id DESC LIMIT @limit`, pgx.StrictNamedArgs{"kind": kind, "limit": limit, "now": now})
	if err != nil {
		return nil, fmt.Errorf("select groups: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var (
			g       Group
			moods   []int
			days    []int
			scanErr error
		)
		if g.Root, scanErr = scanWith(row, &g.Size, &moods, &g.App, &g.Web, &days); scanErr != nil {
			return Group{}, scanErr
		}
		copy(g.Moods[:], moods)
		copy(g.Days[:], days)
		return g, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read groups: %w", err)
	}
	return out, nil
}

// summaryScreens is how many screens Summary lists.
const summaryScreens = 6

func (s pgStore) Summary(ctx context.Context, since, now time.Time) (Summary, error) {
	var out Summary
	args := pgx.StrictNamedArgs{"since": since, "now": now, "before": since.Add(-now.Sub(since))}
	err := s.db.QueryRow(ctx, `SELECT
    coalesce(avg(mood) FILTER (WHERE created_at >= @since), 0)::float8,
    coalesce(avg(mood) FILTER (WHERE created_at < @since), 0)::float8,
    count(*) FILTER (WHERE created_at >= @since),
    count(*) FILTER (WHERE created_at >= @since AND source = 'app'),
    count(*) FILTER (WHERE created_at >= @since AND source = 'web'),
    count(*) FILTER (WHERE created_at >= @since AND kind = 'bug'),
    count(*) FILTER (WHERE created_at >= @since AND kind = 'idea'),
    count(*) FILTER (WHERE created_at >= @since AND kind = 'praise'),
    (SELECT count(*) FROM feedback WHERE status = 'sent'),
    (SELECT count(DISTINCT coalesce(root_id, id)) FROM feedback WHERE status = 'shipped' AND status_at >= @since AND status_at <= @now),
    (SELECT count(*) FROM feedback WHERE status = 'shipped' AND status_at >= @since AND status_at <= @now)
FROM feedback WHERE created_at >= @before AND created_at <= @now`, args).Scan(
		&out.Mood, &out.MoodBefore, &out.Sent, &out.App, &out.Web, &out.Bugs, &out.Ideas, &out.Praise, &out.Waiting,
		&out.ShippedGroups, &out.ShippedPeople)
	if err != nil {
		return Summary{}, fmt.Errorf("count feedback: %w", err)
	}
	// Days run from since; the last ends at now, which it holds.
	rows, err := s.db.Query(ctx, `WITH n AS (SELECT ceil(extract(epoch FROM @now::timestamptz - @since::timestamptz) / 86400)::int AS days)
SELECT coalesce(avg(f.mood), 0)::float8, count(f.id)
FROM n CROSS JOIN generate_series(0, n.days - 1) AS d
LEFT JOIN feedback f ON f.created_at >= @since::timestamptz + d * interval '1 day'
  AND (f.created_at < @since::timestamptz + (d + 1) * interval '1 day' OR (d = n.days - 1 AND f.created_at <= @now::timestamptz))
GROUP BY d ORDER BY d`, pgx.StrictNamedArgs{"since": since, "now": now})
	if err != nil {
		return Summary{}, fmt.Errorf("count days: %w", err)
	}
	for rows.Next() {
		var (
			m float64
			n int
		)
		if err = rows.Scan(&m, &n); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("scan a day: %w", err)
		}
		out.DayMood, out.DaySent = append(out.DayMood, m), append(out.DaySent, n)
	}
	if err = rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read days: %w", err)
	}
	rows, err = s.db.Query(ctx, `SELECT screen, count(*) FROM feedback
WHERE created_at >= @since AND created_at <= @now AND screen IS NOT NULL
GROUP BY screen ORDER BY count(*) DESC, screen LIMIT @screens`, pgx.StrictNamedArgs{"since": since, "now": now, "screens": summaryScreens})
	if err != nil {
		return Summary{}, fmt.Errorf("count screens: %w", err)
	}
	out.Screens, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Screen])
	if err != nil {
		return Summary{}, fmt.Errorf("read screens: %w", err)
	}
	return out, nil
}

// rootOf is the root of id's group and whether id is that root;
// domain.ErrNotFound for no such feedback.
func rootOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (root uuid.UUID, isRoot bool, err error) {
	err = tx.QueryRow(ctx, `SELECT coalesce(root_id, id), root_id IS NULL FROM feedback WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&root, &isRoot)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, false, domain.ErrNotFound
	}
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("read the group: %w", err)
	}
	return root, isRoot, nil
}

// move sets root, c and, where the status changed, status_at on the rows
// where selects, and writes feedback.moved for each row that moved to a
// status that tells.
func move(ctx context.Context, tx pgx.Tx, where string, args pgx.StrictNamedArgs, root uuid.UUID, c domain.StatusChange, at time.Time) error {
	rows, err := tx.Query(ctx, `SELECT id, status FROM feedback `+where+` FOR UPDATE`, args)
	if err != nil {
		return fmt.Errorf("lock the group: %w", err)
	}
	before, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		ID     uuid.UUID
		Status domain.Status
	}])
	if err != nil {
		return fmt.Errorf("read the group: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(before))
	var evs []events.Event
	for _, r := range before {
		ids = append(ids, r.ID)
		if r.Status != c.Status && c.Status.Tells() {
			evs = append(evs, events.Event{ID: uuid.NewV7(), Kind: events.FeedbackMoved, SubjectID: r.ID, OccurredAt: at})
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE feedback SET root_id = nullif(@root, id), status = @status, status_note = @note,
    shipped_version = @version, status_at = CASE WHEN status <> @status THEN @at ELSE status_at END
WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{
		"root": root, "status": c.Status, "note": orNull(c.Note), "version": orNull(c.Version), "at": at, "ids": ids,
	}); err != nil {
		return fmt.Errorf("update the group: %w", err)
	}
	if err = events.Insert(ctx, tx, evs...); err != nil {
		return fmt.Errorf("write events: %w", err)
	}
	return nil
}

func (s pgStore) SetStatus(ctx context.Context, id uuid.UUID, c domain.StatusChange, actor audit.Actor, at time.Time) (Feedback, error) {
	var out Feedback
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockGroups(ctx, tx); err != nil {
			return err
		}
		root, _, err := rootOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = move(ctx, tx, `WHERE id = @root OR root_id = @root`, pgx.StrictNamedArgs{"root": root}, root, c, at); err != nil {
			return err
		}
		if err = audit.Insert(ctx, tx, audit.Entry{
			Actor: actor, Action: audit.FeedbackStatus, Target: audit.Feedback(id), At: at,
			Details: map[string]any{"status": c.Status, "note": c.Note, "version": c.Version},
		}); err != nil {
			return fmt.Errorf("audit: %w", err)
		}
		out, err = one(ctx, tx, id)
		return err
	})
	if err != nil {
		return Feedback{}, fmt.Errorf("set the status: %w", err)
	}
	return out, nil
}

func (s pgStore) Reply(ctx context.Context, id uuid.UUID, text string, actor audit.Actor, at time.Time) (Feedback, error) {
	var out Feedback
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var first bool
		err := tx.QueryRow(ctx, `SELECT reply_body IS NULL FROM feedback WHERE id = @id FOR UPDATE`,
			pgx.StrictNamedArgs{"id": id}).Scan(&first)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock feedback: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE feedback SET reply_body = @text, replied_at = @at, replied_by = @member WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id, "text": text, "at": at, "member": memberOf(actor)}); err != nil {
			return fmt.Errorf("update feedback: %w", err)
		}
		if first {
			if err = events.Insert(ctx, tx, events.Event{ID: uuid.NewV7(), Kind: events.FeedbackReplied, SubjectID: id, OccurredAt: at}); err != nil {
				return fmt.Errorf("write event: %w", err)
			}
		}
		if err = audit.Insert(ctx, tx, audit.Entry{
			Actor: actor, Action: audit.FeedbackReply, Target: audit.Feedback(id), At: at, Details: map[string]any{"first": first},
		}); err != nil {
			return fmt.Errorf("audit: %w", err)
		}
		out, err = one(ctx, tx, id)
		return err
	})
	if err != nil {
		return Feedback{}, fmt.Errorf("reply to feedback: %w", err)
	}
	return out, nil
}

func (s pgStore) Heart(ctx context.Context, id uuid.UUID, hearted bool, actor audit.Actor, at time.Time) (Feedback, error) {
	var out Feedback
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE feedback SET hearted = @hearted WHERE id = @id AND hearted <> @hearted`,
			pgx.StrictNamedArgs{"id": id, "hearted": hearted})
		if err != nil {
			return fmt.Errorf("update feedback: %w", err)
		}
		if tag.RowsAffected() > 0 {
			action := audit.FeedbackHeart
			if !hearted {
				action = audit.FeedbackUnheart
			}
			if err = audit.Insert(ctx, tx, audit.Entry{Actor: actor, Action: action, Target: audit.Feedback(id), At: at}); err != nil {
				return fmt.Errorf("audit: %w", err)
			}
		}
		out, err = one(ctx, tx, id)
		return err
	})
	if err != nil {
		return Feedback{}, fmt.Errorf("heart feedback: %w", err)
	}
	return out, nil
}

func (s pgStore) Join(ctx context.Context, id, other uuid.UUID, actor audit.Actor, at time.Time) (Feedback, error) {
	var out Feedback
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockGroups(ctx, tx); err != nil {
			return err
		}
		from, isRoot, err := rootOf(ctx, tx, id)
		if err != nil {
			return err
		}
		to, _, err := rootOf(ctx, tx, other)
		if err != nil {
			return err
		}
		if from != to {
			var c domain.StatusChange
			if err = tx.QueryRow(ctx, `SELECT status, coalesce(status_note, ''), coalesce(shipped_version, '') FROM feedback WHERE id = @id`,
				pgx.StrictNamedArgs{"id": to}).Scan(&c.Status, &c.Note, &c.Version); err != nil {
				return fmt.Errorf("read the group's status: %w", err)
			}
			where := `WHERE id = @id`
			if isRoot {
				where = `WHERE id = @id OR root_id = @id`
			}
			if err = move(ctx, tx, where, pgx.StrictNamedArgs{"id": id}, to, c, at); err != nil {
				return err
			}
			if err = audit.Insert(ctx, tx, audit.Entry{
				Actor: actor, Action: audit.FeedbackSameAs, Target: audit.Feedback(id), At: at,
				Details: map[string]any{"feedback_id": other},
			}); err != nil {
				return fmt.Errorf("audit: %w", err)
			}
		}
		out, err = one(ctx, tx, id)
		return err
	})
	if err != nil {
		return Feedback{}, fmt.Errorf("join feedback: %w", err)
	}
	return out, nil
}

func (s pgStore) Forget(ctx context.Context, account uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockGroups(ctx, tx); err != nil {
			return err
		}
		// The oldest of another's feedback in each group the account's
		// feedback is the root of.
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (g.root_id) g.root_id, g.id
FROM feedback r JOIN feedback g ON g.root_id = r.id AND g.account_id <> @account
WHERE r.account_id = @account AND r.root_id IS NULL ORDER BY g.root_id, g.id`, pgx.StrictNamedArgs{"account": account})
		if err != nil {
			return fmt.Errorf("find heirs: %w", err)
		}
		heirs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Root, Heir uuid.UUID }])
		if err != nil {
			return fmt.Errorf("read heirs: %w", err)
		}
		for _, h := range heirs {
			if _, err = tx.Exec(ctx, `UPDATE feedback SET root_id = nullif(@heir, id) WHERE root_id = @root AND account_id <> @account`,
				pgx.StrictNamedArgs{"heir": h.Heir, "root": h.Root, "account": account}); err != nil {
				return fmt.Errorf("move a group to its heir: %w", err)
			}
		}
		if _, err = tx.Exec(ctx, `DELETE FROM feedback WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}); err != nil {
			return fmt.Errorf("delete feedback: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("forget the account's feedback: %w", err)
	}
	return nil
}

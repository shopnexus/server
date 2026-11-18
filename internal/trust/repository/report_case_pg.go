package repository

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/trust/domain"
)

type pgCases struct{ db *pgxpool.Pool }

func NewCases(db *pgxpool.Pool) Cases { return pgCases{db} }

// caseCounts are a case's figures over its reports r: the count, the count
// of each reason, and the first and last report's time. scanCase reads them
// in this order.
const caseCounts = `count(*), count(*) FILTER (WHERE r.reason = 'banned'), count(*) FILTER (WHERE r.reason = 'fake'),
       count(*) FILTER (WHERE r.reason = 'scam'), count(*) FILTER (WHERE r.reason = 'other'),
       min(r.created_at), max(r.created_at)`

// openCases is every open case, or account's alone when @account is set:
// its account, its latest report's ID and time, then caseCounts.
const openCases = `
SELECT r.account_id, (array_agg(r.id ORDER BY r.created_at DESC, r.id DESC))[1] AS last_id, max(r.created_at) AS last_at,
       ` + caseCounts + `
FROM reports r
WHERE r.withdrawn_at IS NULL AND r.resolution_id IS NULL AND (@account::uuid IS NULL OR r.account_id = @account)
GROUP BY r.account_id`

// resolvedCases is every resolved case not reopened, or account's alone
// when @account is set: its account and resolution's ID, caseCounts, then
// the resolution.
const resolvedCases = `
SELECT s.account_id, s.id, ` + caseCounts + `, s.outcome, s.member_id, coalesce(m.display_name, ''), s.resolved_at
FROM report_resolutions s
JOIN reports r ON r.resolution_id = s.id AND r.withdrawn_at IS NULL
LEFT JOIN console_members m ON m.id = s.member_id
WHERE s.reopened_at IS NULL AND (@account::uuid IS NULL OR s.account_id = @account)`

// scanCase reads a row of openCases (after its last_at) or resolvedCases.
func scanCase(row pgx.Row, resolved bool) (Case, error) {
	var (
		c                         Case
		banned, fake, scam, other int
		r                         Resolution
	)
	dest := []any{&c.AccountID, &c.Key, &c.Reports, &banned, &fake, &scam, &other, &c.FirstAt, &c.LastAt}
	if resolved {
		dest = append(dest, &r.Outcome, &r.MemberID, &r.MemberName, &r.At)
	}
	if err := row.Scan(dest...); err != nil {
		return Case{}, fmt.Errorf("scan case: %w", err)
	}
	c.Reasons = map[domain.Reason]int{}
	for reason, n := range map[domain.Reason]int{
		domain.ReasonBanned: banned, domain.ReasonFake: fake, domain.ReasonScam: scam, domain.ReasonOther: other,
	} {
		if n > 0 {
			c.Reasons[reason] = n
		}
	}
	if resolved {
		r.ID = c.Key
		c.Resolution = &r
	}
	return c, nil
}

func (s pgCases) Open(ctx context.Context, p page.Request) ([]Case, error) {
	rows, err := s.db.Query(ctx, `WITH open_cases AS (`+openCases+`),
cursor_row AS (SELECT created_at, id FROM reports WHERE id = @before)
SELECT account_id, last_id, n, banned, fake, scam, other, first_at, last_at
FROM open_cases AS o (account_id, last_id, last_at, n, banned, fake, scam, other, first_at, last_on)
WHERE NOT EXISTS (SELECT 1 FROM cursor_row) OR (o.last_at, o.last_id) < (SELECT created_at, id FROM cursor_row)
ORDER BY o.last_at DESC, o.last_id DESC
LIMIT @limit`, pgx.StrictNamedArgs{"account": nil, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select open cases: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Case, error) { return scanCase(row, false) })
	if err != nil {
		return nil, fmt.Errorf("read open cases: %w", err)
	}
	return cs, s.tally(ctx, cs, false)
}

func (s pgCases) CountOpen(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(DISTINCT account_id) FROM reports WHERE withdrawn_at IS NULL AND resolution_id IS NULL`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("count open cases: %w", err)
	}
	return n, nil
}

func (s pgCases) CountByStatus(ctx context.Context, since time.Time) (map[domain.CaseStatus]int, error) {
	return postgres.Counts[domain.CaseStatus](s.db.Query(ctx, `SELECT status, n FROM (
    SELECT 'open' AS status, count(DISTINCT account_id) AS n FROM reports
    WHERE withdrawn_at IS NULL AND resolution_id IS NULL AND created_at >= @since
    UNION ALL
    SELECT 'resolved', count(*) FROM report_resolutions s
    WHERE s.reopened_at IS NULL AND s.resolved_at >= @since
      AND EXISTS (SELECT 1 FROM reports r WHERE r.resolution_id = s.id AND r.withdrawn_at IS NULL)
) c WHERE n > 0`, pgx.StrictNamedArgs{"since": since}))
}

func (s pgCases) Resolved(ctx context.Context, p page.Request) ([]Case, error) {
	rows, err := s.db.Query(ctx, `WITH cursor_row AS (SELECT resolved_at, id FROM report_resolutions WHERE id = @before)
`+resolvedCases+`
  AND (NOT EXISTS (SELECT 1 FROM cursor_row) OR (s.resolved_at, s.id) < (SELECT resolved_at, id FROM cursor_row))
GROUP BY s.id, m.display_name
ORDER BY s.resolved_at DESC, s.id DESC
LIMIT @limit`, pgx.StrictNamedArgs{"account": nil, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select resolved cases: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Case, error) { return scanCase(row, true) })
	if err != nil {
		return nil, fmt.Errorf("read resolved cases: %w", err)
	}
	return cs, s.tally(ctx, cs, true)
}

func (s pgCases) Current(ctx context.Context, account uuid.UUID) (Case, []domain.Report, error) {
	args := pgx.StrictNamedArgs{"account": account}
	c, err := scanCase(s.db.QueryRow(ctx, `SELECT account_id, last_id, n, banned, fake, scam, other, first_at, last_at
FROM (`+openCases+`) AS o (account_id, last_id, last_at, n, banned, fake, scam, other, first_at, last_on)`, args), false)
	resolved := errors.Is(err, pgx.ErrNoRows)
	if resolved {
		c, err = scanCase(s.db.QueryRow(ctx, resolvedCases+`
GROUP BY s.id, m.display_name
ORDER BY s.resolved_at DESC, s.id DESC
LIMIT 1`, args), true)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, nil, ErrNotFound
	}
	if err != nil {
		return Case{}, nil, fmt.Errorf("read the case: %w", err)
	}
	cs := []Case{c}
	if err = s.tally(ctx, cs, resolved); err != nil {
		return Case{}, nil, err
	}
	where, args := `r.account_id = @key AND r.resolution_id IS NULL`, pgx.StrictNamedArgs{"key": account}
	if resolved {
		where, args = `r.resolution_id = @key`, pgx.StrictNamedArgs{"key": c.Key}
	}
	rows, err := s.db.Query(ctx, `SELECT `+reportColumns+` FROM reports r WHERE r.withdrawn_at IS NULL AND `+where+`
ORDER BY r.created_at DESC, r.id DESC`, args)
	if err != nil {
		return Case{}, nil, fmt.Errorf("select the case's reports: %w", err)
	}
	reports, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Report, error) { return scanReport(row) })
	if err != nil {
		return Case{}, nil, fmt.Errorf("read the case's reports: %w", err)
	}
	return cs[0], reports, nil
}

// tally fills in the listings of each case in cs, in one query.
func (s pgCases) tally(ctx context.Context, cs []Case, resolved bool) error {
	if len(cs) == 0 {
		return nil
	}
	keys := make([]uuid.UUID, len(cs))
	at := make(map[uuid.UUID]int, len(cs))
	for i := range cs {
		key := cs[i].AccountID
		if resolved {
			key = cs[i].Key
		}
		keys[i], at[key] = key, i
	}
	group := `r.account_id`
	where := `r.account_id = ANY(@keys) AND r.resolution_id IS NULL`
	if resolved {
		group, where = `r.resolution_id`, `r.resolution_id = ANY(@keys)`
	}
	rows, err := s.db.Query(ctx, `SELECT `+group+`, r.listing_id, count(*), (array_agg(r.id ORDER BY r.id DESC))[1] FROM reports r
WHERE r.withdrawn_at IS NULL AND r.listing_id IS NOT NULL AND `+where+`
GROUP BY `+group+`, r.listing_id`, pgx.StrictNamedArgs{"keys": keys})
	if err != nil {
		return fmt.Errorf("select the cases' listings: %w", err)
	}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var (
			key uuid.UUID
			t   domain.ListingTally
		)
		if scanErr := row.Scan(&key, &t.ListingID, &t.Reports, &t.LatestID); scanErr != nil {
			return struct{}{}, fmt.Errorf("scan listing tally: %w", scanErr)
		}
		cs[at[key]].Listings = append(cs[at[key]].Listings, t)
		return struct{}{}, nil
	})
	if err != nil {
		return fmt.Errorf("read the cases' listings: %w", err)
	}
	return nil
}

// lockCase makes the resolves and reopens of one account wait for each
// other.
func lockCase(ctx context.Context, tx pgx.Tx, account uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('report_case:' || @account::text))`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("lock the case: %w", err)
	}
	return nil
}

func (s pgCases) Resolve(ctx context.Context, account uuid.UUID, r Resolution, entry audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCase(ctx, tx, account); err != nil {
			return err
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reports
WHERE account_id = @account AND withdrawn_at IS NULL AND resolution_id IS NULL)`, pgx.StrictNamedArgs{"account": account}).Scan(&open); err != nil {
			return fmt.Errorf("find the open case: %w", err)
		}
		if !open {
			return domain.ErrCaseNotOpen
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_resolutions (id, account_id, outcome, member_id, resolved_at)
VALUES (@id, @account, @outcome, @member, @at)`, pgx.StrictNamedArgs{
			"id": r.ID, "account": account, "outcome": r.Outcome, "member": r.MemberID, "at": r.At,
		}); err != nil {
			return fmt.Errorf("insert resolution: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE reports SET resolution_id = @id
WHERE account_id = @account AND withdrawn_at IS NULL AND resolution_id IS NULL`, pgx.StrictNamedArgs{"id": r.ID, "account": account}); err != nil {
			return fmt.Errorf("resolve reports: %w", err)
		}
		return audit.Insert(ctx, tx, entry)
	})
	if err != nil {
		return fmt.Errorf("resolve the case: %w", err)
	}
	return nil
}

func (s pgCases) Reopen(ctx context.Context, account uuid.UUID, at time.Time, entry audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockCase(ctx, tx, account); err != nil {
			return err
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM report_resolutions WHERE account_id = @account AND reopened_at IS NULL
ORDER BY resolved_at DESC, id DESC LIMIT 1`, pgx.StrictNamedArgs{"account": account}).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCaseNotResolved
		}
		if err != nil {
			return fmt.Errorf("find the latest resolution: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE report_resolutions SET reopened_at = @at WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
			return fmt.Errorf("reopen resolution: %w", err)
		}
		// A report whose reporter has reported the same target again since
		// stays with the reopened resolution: the newer one stands for it.
		if _, err = tx.Exec(ctx, `UPDATE reports r SET resolution_id = NULL
WHERE r.resolution_id = @id
  AND NOT EXISTS (SELECT 1 FROM reports o
                  WHERE o.reporter_id = r.reporter_id AND o.account_id = r.account_id
                    AND o.listing_id IS NOT DISTINCT FROM r.listing_id
                    AND o.withdrawn_at IS NULL AND o.resolution_id IS NULL)`, pgx.StrictNamedArgs{"id": id}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrConflict
			}
			return fmt.Errorf("reopen reports: %w", err)
		}
		details := map[string]any{"resolution_id": id}
		maps.Copy(details, entry.Details)
		entry.Details = details
		return audit.Insert(ctx, tx, entry)
	})
	if err != nil {
		return fmt.Errorf("reopen the case: %w", err)
	}
	return nil
}

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/stream"
)

type pgNotices struct{ db *pgxpool.Pool }

func NewNotices(db *pgxpool.Pool) Notices { return pgNotices{db} }

// insertNotice adds a notice, or updates the recipient's unread notice with
// the key unless it is newer, so an event delivered late cannot overwrite a
// newer one. A notice without a key has a NULL one, which the partial
// unique index leaves out, so it is always inserted.
const insertNotice = `
INSERT INTO notifications (id, account_id, notice_type, collapse_key, params, event_id, created_at, bumped_at)
VALUES (@id, @account, @type, nullif(@key, ''), @params, @event, @at, @at)
ON CONFLICT (account_id, collapse_key) WHERE read_at IS NULL AND collapse_key IS NOT NULL
DO UPDATE SET notice_type = excluded.notice_type, params = excluded.params, event_id = excluded.event_id,
              bumped_at = excluded.bumped_at
WHERE notifications.bumped_at <= excluded.bumped_at
RETURNING id`

// Add reads what decides the write in one batch: the event's notice and
// a newer one with the key. The write follows in a second; a notice with a
// key learns its ID from the upsert, so its update takes a third.
func (s pgNotices) Add(ctx context.Context, event, fresh uuid.UUID, n domain.Notice, expired time.Time) (uuid.UUID, bool, error) {
	var id uuid.UUID
	late := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var written, newer bool
		b := &pgx.Batch{}
		b.Queue(`SELECT id FROM notifications WHERE account_id = @account AND event_id = @event`,
			pgx.StrictNamedArgs{"account": n.Recipient, "event": event}).QueryRow(func(row pgx.Row) error {
			return scanFound(row, &written, "read the event's notice", &id)
		})
		if n.CollapseKey != "" {
			// A newer notice with the key, read or unread, already told the
			// recipient more than this one would.
			var newest uuid.UUID
			b.Queue(`SELECT id FROM notifications WHERE account_id = @account AND collapse_key = @key AND bumped_at > @at
ORDER BY bumped_at DESC LIMIT 1`, pgx.StrictNamedArgs{"account": n.Recipient, "key": n.CollapseKey, "at": n.At}).
				QueryRow(func(row pgx.Row) error {
					if err := scanFound(row, &newer, "read the newest notice with the key", &newest); err != nil || !newer {
						return err
					}
					if !written {
						id = newest
					}
					return nil
				})
		}
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("read notices: %w", err)
		}
		if written {
			return nil
		}
		if newer {
			late = true
			return nil
		}
		var unread uuid.UUID
		b = &pgx.Batch{}
		// A collapsed notice keeps its first created_at, so age is bumped_at.
		// Up to trimNotices of anyone's go too, a silent account's among them.
		b.Queue(`DELETE FROM notifications WHERE account_id = @account AND bumped_at < @expired`,
			pgx.StrictNamedArgs{"account": n.Recipient, "expired": expired})
		b.Queue(trimExpired, pgx.StrictNamedArgs{"expired": expired, "limit": trimNotices})
		b.Queue(insertNotice, pgx.StrictNamedArgs{
			"id": fresh, "account": n.Recipient, "type": n.Type, "key": n.CollapseKey, "params": n.Params,
			"event": event, "at": n.At,
		}).QueryRow(func(row pgx.Row) error {
			var upserted bool
			err := scanFound(row, &upserted, "upsert notice", &id)
			// A newer notice with the key landed after the read above and
			// stays as it is.
			late = err == nil && !upserted
			return err
		})
		if n.CollapseKey == "" {
			// Nothing collapses a notice without a key, so it is fresh.
			stream.Queue(b, []uuid.UUID{n.Recipient}, stream.Hint{Kind: stream.KindNotification, NotificationID: &fresh})
		} else {
			b.Queue(`SELECT id FROM notifications WHERE account_id = @account AND collapse_key = @key AND read_at IS NULL`,
				pgx.StrictNamedArgs{"account": n.Recipient, "key": n.CollapseKey}).QueryRow(func(row pgx.Row) error {
				if err := row.Scan(&unread); err != nil {
					return fmt.Errorf("read the unread notice: %w", err)
				}
				return nil
			})
		}
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("write notice: %w", err)
		}
		if late {
			// Nothing changed, so the recipient's log gets no update.
			id = unread
			return nil
		}
		if n.CollapseKey == "" {
			return nil
		}
		b = &pgx.Batch{}
		stream.Queue(b, []uuid.UUID{n.Recipient}, stream.Hint{Kind: stream.KindNotification, NotificationID: &id})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("add notice: %w", err)
	}
	return id, late, nil
}

// scanFound scans row into dest and tells in found whether there was one.
func scanFound(row pgx.Row, found *bool, what string, dest ...any) error {
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	*found = true
	return nil
}

// trimNotices is how many expired notices of any account one new notice
// deletes; a notice adds at most one row.
const trimNotices = 100

// trimExpired reads through notifications_bumped; SKIP LOCKED keeps two
// writes from waiting on each other.
const trimExpired = `
DELETE FROM notifications WHERE id IN (
    SELECT id FROM notifications WHERE bumped_at < @expired
    LIMIT @limit FOR UPDATE SKIP LOCKED)`

func (s pgNotices) List(ctx context.Context, account uuid.UUID, p page.Request) ([]Row, error) {
	args := pgx.StrictNamedArgs{"account": account, "fetch": p.Fetch()}
	below := ``
	if p.Cursor != (uuid.UUID{}) {
		args["cursor"] = p.Cursor
		below = ` AND (bumped_at, id) < (SELECT bumped_at, id FROM notifications WHERE id = @cursor AND account_id = @account)`
	}
	rows, err := s.db.Query(ctx, `SELECT id, notice_type, params, read_at IS NOT NULL, bumped_at FROM notifications
WHERE account_id = @account`+below+` ORDER BY bumped_at DESC, id DESC LIMIT @fetch`, args)
	if err != nil {
		return nil, fmt.Errorf("list notices: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Row])
	if err != nil {
		return nil, fmt.Errorf("scan notices: %w", err)
	}
	return out, nil
}

func (s pgNotices) Unread(ctx context.Context, account uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE account_id = @account AND read_at IS NULL`,
		pgx.StrictNamedArgs{"account": account}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

func (s pgNotices) Read(ctx context.Context, account, id uuid.UUID, at time.Time) error {
	tag, err := s.db.Exec(ctx, `UPDATE notifications SET read_at = coalesce(read_at, @at) WHERE id = @id AND account_id = @account`,
		pgx.StrictNamedArgs{"id": id, "account": account, "at": at})
	if err != nil {
		return fmt.Errorf("read notice: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s pgNotices) ReadAll(ctx context.Context, account uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `UPDATE notifications SET read_at = @at
WHERE account_id = @account AND read_at IS NULL RETURNING id`, pgx.StrictNamedArgs{"account": account, "at": at})
	if err != nil {
		return nil, fmt.Errorf("read all: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read all: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

// markUnread picks, per collapse key, the newest of the account's read
// notices among the IDs, and clears its read_at unless the key has an
// unread notice; notices without a key are each their own group.
const markUnread = `
WITH picked AS (
    SELECT DISTINCT ON (coalesce(collapse_key, id::text)) id, collapse_key
    FROM notifications
    WHERE account_id = @account AND id = ANY(@ids) AND read_at IS NOT NULL
    ORDER BY coalesce(collapse_key, id::text), bumped_at DESC, id DESC
)
UPDATE notifications n SET read_at = NULL
FROM picked p
WHERE n.id = p.id
  AND (p.collapse_key IS NULL OR NOT EXISTS (
      SELECT 1 FROM notifications u
      WHERE u.account_id = @account AND u.collapse_key = p.collapse_key AND u.read_at IS NULL))`

func (s pgNotices) MarkUnread(ctx context.Context, account uuid.UUID, ids []uuid.UUID) error {
	args := pgx.StrictNamedArgs{"account": account, "ids": ids}
	_, err := s.db.Exec(ctx, markUnread, args)
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		// A notice of a picked key became unread between the check and the
		// write; the second run sees it and leaves the key alone.
		_, err = s.db.Exec(ctx, markUnread, args)
	}
	if err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	return nil
}

// PutToken takes a share lock on the account's row first, as Settings.Put
// does, so a token racing the account's deletion is forgotten or refused.
func (s pgNotices) PutToken(ctx context.Context, session, account uuid.UUID, token string, at time.Time) error {
	b := &pgx.Batch{}
	postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
	// The phone signed in again, maybe to another account; the old session
	// no longer reaches it.
	b.Queue(`DELETE FROM push_tokens WHERE token = @token AND session_id <> @session`,
		pgx.StrictNamedArgs{"token": token, "session": session})
	b.Queue(`INSERT INTO push_tokens (session_id, account_id, token, updated_at)
VALUES (@session, @account, @token, @at)
ON CONFLICT (session_id) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		pgx.StrictNamedArgs{"session": session, "account": account, "token": token, "at": at})
	// QueueLockLiving's callback aborts, so the batch needs a transaction.
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	}); err != nil {
		return fmt.Errorf("put token: %w", err)
	}
	return nil
}

func (s pgNotices) DeleteToken(ctx context.Context, session uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM push_tokens WHERE session_id = @session`,
		pgx.StrictNamedArgs{"session": session}); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

func (s pgNotices) Forget(ctx context.Context, account uuid.UUID) error {
	b := &pgx.Batch{}
	for _, q := range []string{
		`DELETE FROM notifications WHERE account_id = @account`,
		`DELETE FROM push_tokens WHERE account_id = @account`,
		`DELETE FROM notification_settings WHERE account_id = @account`,
	} {
		b.Queue(q, pgx.StrictNamedArgs{"account": account})
	}
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("forget account: %w", err)
	}
	return nil
}

func (s pgNotices) Tokens(ctx context.Context, account uuid.UUID) ([]Token, error) {
	rows, err := s.db.Query(ctx, `SELECT session_id, token FROM push_tokens WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account})
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Token])
	if err != nil {
		return nil, fmt.Errorf("scan tokens: %w", err)
	}
	return out, nil
}

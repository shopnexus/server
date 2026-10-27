package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/stream"
)

type pgNotices struct{ db *pgxpool.Pool }

func NewNotices(db *pgxpool.Pool) Notices { return pgNotices{db} }

// collapse updates the recipient's unread notice with the key unless it is
// newer, so an event delivered late cannot overwrite a newer one.
const collapse = `
INSERT INTO notifications (id, account_id, notice_type, collapse_key, params, event_id, created_at, bumped_at)
VALUES (@id, @account, @type, @key, @params, @event, @at, @at)
ON CONFLICT (account_id, collapse_key) WHERE read_at IS NULL AND collapse_key IS NOT NULL
DO UPDATE SET notice_type = excluded.notice_type, params = excluded.params, event_id = excluded.event_id,
              bumped_at = excluded.bumped_at
WHERE notifications.bumped_at <= excluded.bumped_at
RETURNING id`

func (s pgNotices) Add(ctx context.Context, event uuid.UUID, n domain.Notice, expired time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT id FROM notifications WHERE account_id = @account AND event_id = @event`,
			pgx.StrictNamedArgs{"account": n.Recipient, "event": event}).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read the event's notice: %w", err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM notifications WHERE account_id = @account AND created_at < @expired`,
			pgx.StrictNamedArgs{"account": n.Recipient, "expired": expired}); err != nil {
			return fmt.Errorf("delete old notices: %w", err)
		}
		args := pgx.StrictNamedArgs{
			"id": uuid.NewV7(), "account": n.Recipient, "type": n.Type, "params": n.Params, "event": event, "at": n.At,
		}
		if n.CollapseKey == "" {
			args["key"] = nil
			if err = tx.QueryRow(ctx, `INSERT INTO notifications
    (id, account_id, notice_type, collapse_key, params, event_id, created_at, bumped_at)
VALUES (@id, @account, @type, @key, @params, @event, @at, @at) RETURNING id`, args).Scan(&id); err != nil {
				return fmt.Errorf("insert notice: %w", err)
			}
		} else {
			args["key"] = n.CollapseKey
			err = tx.QueryRow(ctx, collapse, args).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				// The unread notice is newer and stays as it is.
				err = tx.QueryRow(ctx, `SELECT id FROM notifications
WHERE account_id = @account AND collapse_key = @key AND read_at IS NULL`,
					pgx.StrictNamedArgs{"account": n.Recipient, "key": n.CollapseKey}).Scan(&id)
			}
			if err != nil {
				return fmt.Errorf("upsert notice: %w", err)
			}
		}
		return stream.Notify(ctx, tx, []uuid.UUID{n.Recipient}, stream.Hint{Kind: stream.KindNotification, NotificationID: &id})
	})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("add notice: %w", err)
	}
	return id, nil
}

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
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err = rows.Scan(&r.ID, &r.Type, &r.Params, &r.Read, &r.BumpedAt); err != nil {
			return nil, fmt.Errorf("scan notice: %w", err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("notice rows: %w", err)
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

func (s pgNotices) ReadAll(ctx context.Context, account uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `UPDATE notifications SET read_at = @at WHERE account_id = @account AND read_at IS NULL`,
		pgx.StrictNamedArgs{"account": account, "at": at}); err != nil {
		return fmt.Errorf("read all: %w", err)
	}
	return nil
}

func (s pgNotices) PutToken(ctx context.Context, session, account uuid.UUID, token string, at time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO push_tokens (session_id, account_id, token, updated_at)
VALUES (@session, @account, @token, @at)
ON CONFLICT (session_id) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		pgx.StrictNamedArgs{"session": session, "account": account, "token": token, "at": at}); err != nil {
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

func (s pgNotices) Tokens(ctx context.Context, account uuid.UUID) ([]Token, error) {
	rows, err := s.db.Query(ctx, `SELECT session_id, token FROM push_tokens WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account})
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err = rows.Scan(&t.Session, &t.Token); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		out = append(out, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("token rows: %w", err)
	}
	return out, nil
}

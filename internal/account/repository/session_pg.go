package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

type pgSessions struct{ db *pgxpool.Pool }

func NewSessions(db *pgxpool.Pool) Sessions { return pgSessions{db} }

const sessionColumns = `id, account_id, refresh_hash, prev_refresh_hash, coalesce(device_name, ''),
       auth_time, created_at, last_used_at, expires_at, revoked_at, coalesce(revoke_reason, ''), write_id`

func scanSession(row pgx.Row) (domain.Session, error) {
	var st domain.SessionState
	err := row.Scan(&st.ID, &st.AccountID, &st.RefreshHash, &st.PrevRefreshHash, &st.DeviceName,
		&st.AuthTime, &st.CreatedAt, &st.LastUsedAt, &st.ExpiresAt, &st.RevokedAt, &st.RevokeReason, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("select session: %w", err)
	}
	return domain.RestoreSession(st), nil
}

const createSession = `
INSERT INTO sessions (id, account_id, refresh_hash, device_name, auth_time, created_at, last_used_at, expires_at, request_key, write_id)
VALUES (@id, @account_id, @refresh_hash, NULLIF(@device_name, ''), @auth_time, @created_at, @created_at, @expires_at,
        @request_key, @id)
ON CONFLICT DO NOTHING`

func (s pgSessions) Create(ctx context.Context, x domain.Session, key uuid.UUID) (uuid.UUID, error) {
	var requestKey *uuid.UUID
	if key != uuid.Nil() {
		requestKey = &key
	}
	b := &pgx.Batch{}
	b.Queue(createSession, pgx.StrictNamedArgs{
		"id": x.ID(), "account_id": x.AccountID(), "refresh_hash": x.RefreshHash(), "device_name": x.DeviceName(),
		"auth_time": x.AuthTime(), "created_at": x.CreatedAt(), "expires_at": x.ExpiresAt(), "request_key": requestKey,
	})
	var id uuid.UUID
	b.Queue(`SELECT id FROM sessions WHERE id = @id OR (account_id = @account_id AND request_key = @request_key)`,
		pgx.StrictNamedArgs{"id": x.ID(), "account_id": x.AccountID(), "request_key": requestKey}).
		QueryRow(func(row pgx.Row) error { return row.Scan(&id) })
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return uuid.Nil(), fmt.Errorf("insert session: %w", err)
	}
	return id, nil
}

func (s pgSessions) Get(ctx context.Context, id uuid.UUID) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgSessions) FindByRefresh(ctx context.Context, hash []byte) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE refresh_hash = @hash`, pgx.StrictNamedArgs{"hash": hash}))
}

func (s pgSessions) FindByPrevRefresh(ctx context.Context, hash []byte) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE prev_refresh_hash = @hash LIMIT 1`, pgx.StrictNamedArgs{"hash": hash}))
}

const saveSession = `
UPDATE sessions
SET refresh_hash = @refresh_hash, prev_refresh_hash = @prev_refresh_hash, last_used_at = @last_used_at,
    revoked_at = @revoked_at, revoke_reason = nullif(@revoke_reason, ''), write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
RETURNING auth_time`

func (s pgSessions) Save(ctx context.Context, x *domain.Session, writeID uuid.UUID) error {
	var authTime time.Time
	err := s.db.QueryRow(ctx, saveSession, pgx.StrictNamedArgs{
		"id": x.ID(), "read_write_id": x.WriteID(), "write_id": writeID,
		"refresh_hash": x.RefreshHash(), "prev_refresh_hash": x.PrevRefreshHash(),
		"last_used_at": x.LastUsedAt(), "revoked_at": x.RevokedAt(), "revoke_reason": string(x.RevokeReason()),
	}).Scan(&authTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	x.Saved(writeID, authTime)
	return nil
}

func (s pgSessions) Reauthenticate(ctx context.Context, id uuid.UUID, at time.Time) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `UPDATE sessions SET auth_time = greatest(auth_time, @at)
WHERE id = @id AND revoked_at IS NULL AND expires_at > @at AND last_used_at > @idle_since
RETURNING `+sessionColumns, pgx.StrictNamedArgs{"id": id, "at": at, "idle_since": at.Add(-domain.SessionIdleLimit)}))
}

func (s pgSessions) RevokeOthers(ctx context.Context, accountID, keep uuid.UUID, now time.Time, writeID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
UPDATE sessions SET revoked_at = @now, revoke_reason = @reason, write_id = @write_id
WHERE account_id = @account_id AND id <> @keep AND revoked_at IS NULL`,
		pgx.StrictNamedArgs{"account_id": accountID, "keep": keep, "now": now, "reason": string(domain.RevokedSignOut), "write_id": writeID})
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

const listActiveSessions = `
SELECT id, coalesce(device_name, ''), created_at, last_used_at
FROM sessions
WHERE account_id = @account_id AND revoked_at IS NULL AND expires_at > @now AND last_used_at > @idle_since
  AND id < @before
ORDER BY id DESC
LIMIT @limit`

func (s pgSessions) ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, p page.Request) ([]ActiveSession, error) {
	rows, err := s.db.Query(ctx, listActiveSessions, pgx.StrictNamedArgs{
		"account_id": accountID, "now": now, "idle_since": now.Add(-domain.SessionIdleLimit), "before": p.Before(), "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select active sessions: %w", err)
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ActiveSession])
	if err != nil {
		return nil, fmt.Errorf("read active sessions: %w", err)
	}
	return views, nil
}

func (s pgSessions) ActiveAmong(ctx context.Context, ids []uuid.UUID, now time.Time) (map[uuid.UUID]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM sessions
WHERE id = ANY(@ids) AND revoked_at IS NULL AND expires_at > @now AND last_used_at > @idle_since`,
		pgx.StrictNamedArgs{"ids": ids, "now": now, "idle_since": now.Add(-domain.SessionIdleLimit)})
	if err != nil {
		return nil, fmt.Errorf("select active sessions: %w", err)
	}
	active, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read active sessions: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(active))
	for _, id := range active {
		out[id] = true
	}
	return out, nil
}

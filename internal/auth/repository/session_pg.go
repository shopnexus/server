package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgSessions struct{ db *pgxpool.Pool }

func NewSessions(db *pgxpool.Pool) Sessions { return pgSessions{db} }

const columns = `id, account_id, refresh_hash, prev_refresh_hash, coalesce(device_name, ''),
       auth_time, created_at, last_used_at, expires_at, revoked_at, write_id`

func scanSession(row pgx.Row) (domain.Session, error) {
	var (
		id, accountID, writeID                     uuid.UUID
		refreshHash, prevRefreshHash               []byte
		deviceName                                 string
		authTime, createdAt, lastUsedAt, expiresAt time.Time
		revokedAt                                  *time.Time
	)
	err := row.Scan(&id, &accountID, &refreshHash, &prevRefreshHash, &deviceName,
		&authTime, &createdAt, &lastUsedAt, &expiresAt, &revokedAt, &writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("select session: %w", err)
	}
	return domain.RestoreSession(id, accountID, refreshHash, prevRefreshHash, deviceName,
		authTime, createdAt, lastUsedAt, expiresAt, revokedAt, writeID), nil
}

const createSession = `
INSERT INTO sessions (id, account_id, refresh_hash, device_name, auth_time, created_at, last_used_at, expires_at, write_id)
VALUES (@id, @account_id, @refresh_hash, NULLIF(@device_name, ''), @auth_time, @created_at, @created_at, @expires_at, @id)
ON CONFLICT (id) DO NOTHING`

func (s pgSessions) Create(ctx context.Context, x domain.Session) (bool, error) {
	tag, err := s.db.Exec(ctx, createSession, pgx.StrictNamedArgs{
		"id": x.ID(), "account_id": x.AccountID(), "refresh_hash": x.RefreshHash(), "device_name": x.DeviceName(),
		"auth_time": x.AuthTime(), "created_at": x.CreatedAt(), "expires_at": x.ExpiresAt(),
	})
	if err != nil {
		return false, fmt.Errorf("insert session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgSessions) Get(ctx context.Context, id uuid.UUID) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgSessions) FindByRefresh(ctx context.Context, hash []byte) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE refresh_hash = @hash`, pgx.StrictNamedArgs{"hash": hash}))
}

func (s pgSessions) FindByPrevRefresh(ctx context.Context, hash []byte) (domain.Session, error) {
	return scanSession(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE prev_refresh_hash = @hash LIMIT 1`, pgx.StrictNamedArgs{"hash": hash}))
}

const saveSession = `
UPDATE sessions
SET refresh_hash = @refresh_hash, prev_refresh_hash = @prev_refresh_hash, last_used_at = @last_used_at,
    revoked_at = @revoked_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgSessions) Save(ctx context.Context, x *domain.Session, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveSession, pgx.StrictNamedArgs{
		"id": x.ID(), "read_write_id": x.WriteID(), "write_id": writeID,
		"refresh_hash": x.RefreshHash(), "prev_refresh_hash": x.PrevRefreshHash(),
		"last_used_at": x.LastUsedAt(), "revoked_at": x.RevokedAt(),
	})
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	x.Saved(writeID)
	return nil
}

func (s pgSessions) RevokeAll(ctx context.Context, accountID uuid.UUID, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE sessions SET revoked_at = @now, write_id = uuidv7()
WHERE account_id = @account_id AND revoked_at IS NULL`, pgx.StrictNamedArgs{"account_id": accountID, "now": now})
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

const listActive = `
SELECT id, coalesce(device_name, ''), created_at, last_used_at
FROM sessions
WHERE account_id = @account_id AND revoked_at IS NULL AND expires_at > @now AND last_used_at > @idle_since
  AND id < @before
ORDER BY id DESC
LIMIT @limit`

func (s pgSessions) ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, p page.Request) ([]ActiveSession, error) {
	rows, err := s.db.Query(ctx, listActive, pgx.StrictNamedArgs{
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
	rows, err := s.db.Query(ctx, `SELECT id FROM sessions WHERE id = ANY(@ids) AND revoked_at IS NULL AND expires_at > @now`,
		pgx.StrictNamedArgs{"ids": ids, "now": now})
	if err != nil {
		return nil, fmt.Errorf("read active sessions: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out[id] = true
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("session rows: %w", err)
	}
	return out, nil
}

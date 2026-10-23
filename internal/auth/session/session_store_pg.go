package session

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

const columns = `id, account_id, refresh_hash, prev_refresh_hash, coalesce(device_name, ''),
       auth_time, created_at, last_used_at, expires_at, revoked_at, write_id`

func scan(row pgx.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.id, &s.accountID, &s.refreshHash, &s.prevRefreshHash, &s.deviceName,
		&s.authTime, &s.createdAt, &s.lastUsedAt, &s.expiresAt, &s.revokedAt, &s.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("select session: %w", err)
	}
	return s, nil
}

const createSession = `
INSERT INTO sessions (id, account_id, refresh_hash, device_name, auth_time, created_at, last_used_at, expires_at, write_id)
VALUES (@id, @account_id, @refresh_hash, NULLIF(@device_name, ''), @auth_time, @created_at, @created_at, @expires_at, @id)
ON CONFLICT (id) DO NOTHING`

func (s pgStore) Create(ctx context.Context, x Session) (bool, error) {
	tag, err := s.db.Exec(ctx, createSession, pgx.StrictNamedArgs{
		"id": x.id, "account_id": x.accountID, "refresh_hash": x.refreshHash, "device_name": x.deviceName,
		"auth_time": x.authTime, "created_at": x.createdAt, "expires_at": x.expiresAt,
	})
	if err != nil {
		return false, fmt.Errorf("insert session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgStore) FindByRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE refresh_hash = @hash`, pgx.StrictNamedArgs{"hash": hash}))
}

func (s pgStore) FindByPrevRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE prev_refresh_hash = @hash LIMIT 1`, pgx.StrictNamedArgs{"hash": hash}))
}

const saveSession = `
UPDATE sessions
SET refresh_hash = @refresh_hash, prev_refresh_hash = @prev_refresh_hash, last_used_at = @last_used_at,
    revoked_at = @revoked_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgStore) Save(ctx context.Context, x *Session, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveSession, pgx.StrictNamedArgs{
		"id": x.id, "read_write_id": x.writeID, "write_id": writeID,
		"refresh_hash": x.refreshHash, "prev_refresh_hash": x.prevRefreshHash,
		"last_used_at": x.lastUsedAt, "revoked_at": x.revokedAt,
	})
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	x.writeID = writeID
	return nil
}

func (s pgStore) RevokeAll(ctx context.Context, accountID uuid.UUID, now time.Time) error {
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

func (s pgStore) ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, p page.Request) ([]View, error) {
	rows, err := s.db.Query(ctx, listActive, pgx.StrictNamedArgs{
		"account_id": accountID, "now": now, "idle_since": now.Add(-IdleLimit), "before": p.Before(), "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select active sessions: %w", err)
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[View])
	if err != nil {
		return nil, fmt.Errorf("read active sessions: %w", err)
	}
	return views, nil
}

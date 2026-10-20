package session

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	return s, err
}

const createSession = `
INSERT INTO sessions (id, account_id, refresh_hash, device_name, auth_time, created_at, last_used_at, expires_at, write_id)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $6, $7, $1)
ON CONFLICT (id) DO NOTHING`

func (s pgStore) Create(ctx context.Context, x Session) (bool, error) {
	tag, err := s.db.Exec(ctx, createSession, x.id, x.accountID, x.refreshHash, x.deviceName, x.authTime, x.createdAt, x.expiresAt)
	return tag.RowsAffected() == 1, err
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE id = $1`, id))
}

func (s pgStore) FindByRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE refresh_hash = $1`, hash))
}

func (s pgStore) FindByPrevRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM sessions WHERE prev_refresh_hash = $1 LIMIT 1`, hash))
}

const saveSession = `
UPDATE sessions SET refresh_hash = $3, prev_refresh_hash = $4, last_used_at = $5, revoked_at = $6, write_id = $7
WHERE id = $1 AND (write_id = $2 OR write_id = $7)`

func (s pgStore) Save(ctx context.Context, x *Session, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveSession, x.id, x.writeID, x.refreshHash, x.prevRefreshHash, x.lastUsedAt, x.revokedAt, writeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	x.writeID = writeID
	return nil
}

func (s pgStore) RevokeAll(ctx context.Context, accountID uuid.UUID, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE sessions SET revoked_at = $2, write_id = uuidv7()
WHERE account_id = $1 AND revoked_at IS NULL`, accountID, now)
	return err
}

const listActive = `
SELECT id, coalesce(device_name, ''), created_at, last_used_at
FROM sessions
WHERE account_id = $1 AND revoked_at IS NULL AND expires_at > $2 AND last_used_at > $3
  AND ($4::uuid IS NULL OR id < $4)
ORDER BY id DESC
LIMIT $5`

func (s pgStore) ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, before uuid.UUID, limit int) ([]View, error) {
	var cursor any
	if before != (uuid.UUID{}) {
		cursor = before
	}
	rows, err := s.db.Query(ctx, listActive, accountID, now, now.Add(-IdleLimit), cursor, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (View, error) {
		var v View
		err := r.Scan(&v.ID, &v.DeviceName, &v.CreatedAt, &v.LastUsedAt)
		return v, err
	})
}

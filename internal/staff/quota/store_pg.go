package quota

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
)

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

func (s pgStore) Get(ctx context.Context, key []byte) (Quota, error) {
	q := New(key)
	err := s.db.QueryRow(ctx, `SELECT failures, locked_until, updated_at, write_id FROM staff_login_quotas WHERE key = @key`,
		pgx.StrictNamedArgs{"key": key}).Scan(&q.failures, &q.lockedUntil, &q.updatedAt, &q.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return New(key), nil
	}
	if err != nil {
		return Quota{}, fmt.Errorf("select staff login quota: %w", err)
	}
	return q, nil
}

// saveQuota inserts a new quota or updates one whose write_id matches. A
// quota never saved carries the zero UUID, which no row holds, so two first
// saves of one key conflict.
const saveQuota = `
INSERT INTO staff_login_quotas (key, failures, locked_until, updated_at, write_id)
VALUES (@key, @failures, @locked_until, @updated_at, @write_id)
ON CONFLICT (key) DO UPDATE
SET failures = EXCLUDED.failures, locked_until = EXCLUDED.locked_until, updated_at = EXCLUDED.updated_at,
    write_id = EXCLUDED.write_id
WHERE staff_login_quotas.write_id = @read_write_id OR staff_login_quotas.write_id = @write_id`

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func upsert(ctx context.Context, q execer, x *Quota, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveQuota, pgx.StrictNamedArgs{
		"key": x.key, "failures": x.failures, "locked_until": x.lockedUntil, "updated_at": x.updatedAt,
		"read_write_id": x.writeID, "write_id": writeID,
	})
	if err != nil {
		return fmt.Errorf("upsert staff login quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgStore) Save(ctx context.Context, q *Quota, writeID uuid.UUID) error {
	if err := upsert(ctx, s.db, q, writeID); err != nil {
		return err
	}
	q.writeID = writeID
	return nil
}

func (s pgStore) SaveAudited(ctx context.Context, q *Quota, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := upsert(ctx, tx, q, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save staff login quota with audit: %w", err)
	}
	q.writeID = writeID
	return nil
}

func (s pgStore) LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error) {
	rows, err := s.db.Query(ctx, `SELECT key, locked_until FROM staff_login_quotas WHERE key = ANY(@keys) AND locked_until > @now`,
		pgx.StrictNamedArgs{"keys": keys, "now": now})
	if err != nil {
		return nil, fmt.Errorf("select staff login locks: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var (
			key   []byte
			until time.Time
		)
		if err := rows.Scan(&key, &until); err != nil {
			return nil, fmt.Errorf("read staff login lock: %w", err)
		}
		out[string(key)] = until
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read staff login locks: %w", err)
	}
	return out, nil
}

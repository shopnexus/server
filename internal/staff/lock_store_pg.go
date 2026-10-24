package staff

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
)

type pgLockStore struct{ db *pgxpool.Pool }

func NewPGLockStore(db *pgxpool.Pool) LockStore { return pgLockStore{db} }

// chargeLock counts an attempt while the email is unlocked and under the
// budget, or starts a fresh count once a lock has run out. The row lock of
// ON CONFLICT DO UPDATE serialises concurrent attempts on one email, and
// each sees the count the previous one left.
const chargeLock = `
INSERT INTO staff_login_locks AS l (key, failures, locked_until, updated_at)
VALUES (@key, 1, NULL, @now)
ON CONFLICT (key) DO UPDATE
SET failures     = CASE WHEN l.locked_until <= @now THEN 1 ELSE l.failures + 1 END,
    locked_until = CASE WHEN l.locked_until <= @now THEN NULL ELSE l.locked_until END,
    updated_at   = @now
WHERE (l.locked_until IS NULL AND l.failures < @max) OR l.locked_until <= @now
RETURNING failures`

func (s pgLockStore) Charge(ctx context.Context, key []byte, now time.Time) error {
	var failures int
	err := s.db.QueryRow(ctx, chargeLock, pgx.StrictNamedArgs{"key": key, "now": now, "max": MaxFailures}).Scan(&failures)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("charge staff login lock: %w", err)
	}
	var until *time.Time
	if err := s.db.QueryRow(ctx, `SELECT locked_until FROM staff_login_locks WHERE key = @key`,
		pgx.StrictNamedArgs{"key": key}).Scan(&until); err != nil {
		return fmt.Errorf("select staff login lock: %w", err)
	}
	// Not locked yet means attempts still in flight hold the budget; one of
	// them locks the email or refunds within a second.
	if until == nil || !until.After(now) {
		return locked(time.Second)
	}
	return locked(until.Sub(now))
}

func (s pgLockStore) Refund(ctx context.Context, key []byte, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE staff_login_locks SET failures = greatest(failures - 1, 0), updated_at = @now
WHERE key = @key AND locked_until IS NULL`, pgx.StrictNamedArgs{"key": key, "now": now})
	if err != nil {
		return fmt.Errorf("refund staff login lock: %w", err)
	}
	return nil
}

func (s pgLockStore) Fail(ctx context.Context, key []byte, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE staff_login_locks SET failures = 0, locked_until = @until, updated_at = @now
WHERE key = @key AND locked_until IS NULL AND failures >= @max`,
		pgx.StrictNamedArgs{"key": key, "until": now.Add(LockFor), "now": now, "max": MaxFailures})
	if err != nil {
		return fmt.Errorf("lock staff login: %w", err)
	}
	return nil
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func clearLock(ctx context.Context, q execer, key []byte, now time.Time) error {
	_, err := q.Exec(ctx, `UPDATE staff_login_locks SET failures = 0, locked_until = NULL, updated_at = @now WHERE key = @key`,
		pgx.StrictNamedArgs{"key": key, "now": now})
	if err != nil {
		return fmt.Errorf("clear staff login lock: %w", err)
	}
	return nil
}

func (s pgLockStore) Clear(ctx context.Context, key []byte, now time.Time) error {
	return clearLock(ctx, s.db, key, now)
}

func (s pgLockStore) Unlock(ctx context.Context, key []byte, now time.Time, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := clearLock(ctx, tx, key, now); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("unlock staff login: %w", err)
	}
	return nil
}

func (s pgLockStore) LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error) {
	rows, err := s.db.Query(ctx, `SELECT key, locked_until FROM staff_login_locks WHERE key = ANY(@keys) AND locked_until > @now`,
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

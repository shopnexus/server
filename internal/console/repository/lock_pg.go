package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

type pgLocks struct{ db *pgxpool.Pool }

func NewLocks(db *pgxpool.Pool) Locks { return pgLocks{db} }

// chargeLock counts an attempt while the email is unlocked and under the
// budget, or starts a fresh count once a lock has run out. The row lock of
// ON CONFLICT DO UPDATE serialises concurrent attempts on one email, and
// each sees the count the previous one left.
const chargeLock = `
INSERT INTO console_login_locks AS l (email_hash, failures, locked_until, updated_at)
VALUES (@email_hash, 1, NULL, @now)
ON CONFLICT (email_hash) DO UPDATE
SET failures     = CASE WHEN l.locked_until <= @now THEN 1 ELSE l.failures + 1 END,
    locked_until = CASE WHEN l.locked_until <= @now THEN NULL ELSE l.locked_until END,
    updated_at   = @now
WHERE (l.locked_until IS NULL AND l.failures < @max) OR l.locked_until <= @now
RETURNING failures`

func (s pgLocks) Charge(ctx context.Context, key []byte, now time.Time) error {
	var failures int
	err := s.db.QueryRow(ctx, chargeLock, pgx.StrictNamedArgs{"email_hash": key, "now": now, "max": domain.MaxFailures}).Scan(&failures)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("charge console login lock: %w", err)
	}
	var until *time.Time
	if err := s.db.QueryRow(ctx, `SELECT locked_until FROM console_login_locks WHERE email_hash = @email_hash`,
		pgx.StrictNamedArgs{"email_hash": key}).Scan(&until); err != nil {
		return fmt.Errorf("select console login lock: %w", err)
	}
	// Not locked yet means attempts still in flight hold the budget; one of
	// them locks the email or refunds within a second.
	if until == nil || !until.After(now) {
		return domain.Locked(time.Second)
	}
	return domain.Locked(until.Sub(now))
}

func (s pgLocks) Refund(ctx context.Context, key []byte, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE console_login_locks SET failures = greatest(failures - 1, 0), updated_at = @now
WHERE email_hash = @email_hash AND locked_until IS NULL`, pgx.StrictNamedArgs{"email_hash": key, "now": now})
	if err != nil {
		return fmt.Errorf("refund console login lock: %w", err)
	}
	return nil
}

func (s pgLocks) Fail(ctx context.Context, key []byte, now time.Time) error {
	_, err := s.db.Exec(ctx, `
UPDATE console_login_locks SET failures = 0, locked_until = @until, updated_at = @now
WHERE email_hash = @email_hash AND locked_until IS NULL AND failures >= @max`,
		pgx.StrictNamedArgs{"email_hash": key, "until": now.Add(domain.LockFor), "now": now, "max": domain.MaxFailures})
	if err != nil {
		return fmt.Errorf("lock console login: %w", err)
	}
	return nil
}

const clearLock = `UPDATE console_login_locks SET failures = 0, locked_until = NULL, updated_at = @now WHERE email_hash = @email_hash`

func (s pgLocks) Clear(ctx context.Context, key []byte, now time.Time) error {
	if _, err := s.db.Exec(ctx, clearLock, pgx.StrictNamedArgs{"email_hash": key, "now": now}); err != nil {
		return fmt.Errorf("clear console login lock: %w", err)
	}
	return nil
}

func (s pgLocks) Unlock(ctx context.Context, key []byte, now time.Time, e audit.Entry) error {
	// No check aborts, so the batch commits as one on the pool.
	b := &pgx.Batch{}
	b.Queue(clearLock, pgx.StrictNamedArgs{"email_hash": key, "now": now}).Fn = func(br pgx.BatchResults) error {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("clear console login lock: %w", err)
		}
		return nil
	}
	audit.Queue(b, e)
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("unlock console login: %w", err)
	}
	return nil
}

func (s pgLocks) LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error) {
	rows, err := s.db.Query(ctx, `SELECT email_hash, locked_until FROM console_login_locks WHERE email_hash = ANY(@email_hashes) AND locked_until > @now`,
		pgx.StrictNamedArgs{"email_hashes": keys, "now": now})
	if err != nil {
		return nil, fmt.Errorf("select console login locks: %w", err)
	}
	type lock struct {
		Key   []byte
		Until time.Time
	}
	locks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[lock])
	if err != nil {
		return nil, fmt.Errorf("read console login locks: %w", err)
	}
	out := make(map[string]time.Time, len(locks))
	for _, l := range locks {
		out[string(l.Key)] = l.Until
	}
	return out, nil
}

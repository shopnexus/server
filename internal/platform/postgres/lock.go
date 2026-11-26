package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueLock queues a transaction-scoped advisory lock on scope and ids,
// keyed "scope:id:…", so locks of two scopes never share a key.
func QueueLock(b *pgx.Batch, scope string, ids ...any) {
	lockAs(b, "pg_advisory_xact_lock", scope, ids)
}

// QueueLockShared is QueueLock's shared mode: shared holders run together
// and wait only for an exclusive one.
func QueueLockShared(b *pgx.Batch, scope string, ids ...any) {
	lockAs(b, "pg_advisory_xact_lock_shared", scope, ids)
}

func lockAs(b *pgx.Batch, fn, scope string, ids []any) {
	parts := make([]string, 0, 1+len(ids))
	parts = append(parts, scope)
	for _, id := range ids {
		parts = append(parts, fmt.Sprint(id))
	}
	b.Queue(`SELECT `+fn+`(hashtextextended(@key, 0))`,
		pgx.StrictNamedArgs{"key": strings.Join(parts, ":")}).Fn = ExecAs("lock "+scope, nil)
}

// WithLock runs fn holding a session advisory lock on scope, so that one
// process at a time runs it across replicas. The lock waits on ctx. When
// the unlock fails, the connection is closed, which drops the lock.
func WithLock(ctx context.Context, pool *pgxpool.Pool, scope string, fn func(context.Context) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended(@key, 0))`, pgx.StrictNamedArgs{"key": scope}); err != nil {
		conn.Release()
		return fmt.Errorf("lock %s: %w", scope, err)
	}
	err = fn(ctx)
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockWait)
	defer cancel()
	if _, uerr := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended(@key, 0))`, pgx.StrictNamedArgs{"key": scope}); uerr != nil {
		_ = conn.Hijack().Close(unlockCtx)
		return errors.Join(err, fmt.Errorf("unlock %s: %w", scope, uerr))
	}
	conn.Release()
	return err
}

// unlockWait bounds the unlock of WithLock.
const unlockWait = 5 * time.Second

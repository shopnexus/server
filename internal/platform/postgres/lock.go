package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// QueueLock queues a transaction-scoped advisory lock on scope and ids,
// keyed "scope:id:…", so locks of two scopes never share a key.
func QueueLock(b *pgx.Batch, scope string, ids ...any) {
	queueLock(b, "pg_advisory_xact_lock", scope, ids)
}

// QueueLockShared is QueueLock's shared mode: shared holders run together
// and wait only for an exclusive one.
func QueueLockShared(b *pgx.Batch, scope string, ids ...any) {
	queueLock(b, "pg_advisory_xact_lock_shared", scope, ids)
}

func queueLock(b *pgx.Batch, fn, scope string, ids []any) {
	parts := []string{scope}
	for _, id := range ids {
		parts = append(parts, fmt.Sprint(id))
	}
	b.Queue(`SELECT `+fn+`(hashtextextended(@key, 0))`,
		pgx.StrictNamedArgs{"key": strings.Join(parts, ":")}).Fn = ExecAs("lock "+scope, nil)
}

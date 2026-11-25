package postgres

import (
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// QueueLockLiving queues a share lock on account's row into b, a batch sent
// in a tx; the batch fails with gone when the account is deleted or missing.
// Every write that keeps a row of an account's queues it first, so a write
// racing the account's erasure either commits before it, and the module's
// cleanup on account.deleted removes the row, or waits and sees the
// tombstone. The rest of the batch has run by then, so gone must abort the
// tx; a write that skips quietly maps gone to nil after the rollback.
func QueueLockLiving(b *pgx.Batch, account uuid.UUID, gone error) {
	b.Queue(`SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`,
		pgx.StrictNamedArgs{"id": account}).QueryRow(func(row pgx.Row) error {
		var deleted bool
		err := row.Scan(&deleted)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && deleted {
			return gone
		}
		if err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		return nil
	})
}

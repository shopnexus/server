package postgres

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const lockLiving = `SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`

// LockLiving takes a share lock on account's row in tx and tells whether
// the account lives: false for a deleted account or none. Every write that
// keeps a row of an account's calls it first, so a write racing the
// account's erasure either commits before it, and the module's cleanup on
// account.deleted removes the row, or waits for it and sees the tombstone.
func LockLiving(ctx context.Context, tx pgx.Tx, account uuid.UUID) (bool, error) {
	var deleted bool
	err := tx.QueryRow(ctx, lockLiving, pgx.StrictNamedArgs{"id": account}).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock account: %w", err)
	}
	return !deleted, nil
}

// QueueLockLiving queues LockLiving's lock into b, a batch sent in a tx:
// the batch fails with gone when the account is deleted or missing. The
// rest of the batch has run by then, so gone must abort the tx; a write
// that skips quietly maps gone to nil after the rollback.
func QueueLockLiving(b *pgx.Batch, account uuid.UUID, gone error) {
	b.Queue(lockLiving, pgx.StrictNamedArgs{"id": account}).QueryRow(func(row pgx.Row) error {
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

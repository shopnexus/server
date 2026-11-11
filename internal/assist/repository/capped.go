package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
)

// insertCapped runs insert, which writes read id of account to table
// (video_reads or link_reads) and notifies, in a transaction that takes a share lock on the account's row
// (a deleted account is accountdomain.ErrSessionRevoked), locks the
// account's reads in table, and counts those created after at minus
// domain.ReadWindow: domain.ReadLimit of them is domain.ErrReadLimit. A
// read already stored passes without a count or a notice, so a retried
// step succeeds.
func insertCapped(ctx context.Context, db *pgxpool.Pool, table string, id, account uuid.UUID, at time.Time,
	insert func(tx pgx.Tx) error,
) error {
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// A share lock on the account's row: an insert that commits after
		// Erase waits for it and sees the tombstone.
		var deleted bool
		txErr := tx.QueryRow(ctx, `SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`,
			pgx.StrictNamedArgs{"id": account}).Scan(&deleted)
		if txErr != nil && !errors.Is(txErr, pgx.ErrNoRows) {
			return fmt.Errorf("lock account: %w", txErr)
		}
		if deleted {
			return accountdomain.ErrSessionRevoked
		}
		if _, txErr = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@table || ':' || @account::text))`,
			pgx.StrictNamedArgs{"table": table, "account": account}); txErr != nil {
			return fmt.Errorf("lock reads: %w", txErr)
		}
		var (
			stored bool
			n      int
		)
		if txErr = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = @id),
       (SELECT count(*) FROM `+table+` WHERE account_id = @account AND created_at > @since)`,
			pgx.StrictNamedArgs{"id": id, "account": account, "since": at.Add(-domain.ReadWindow)}).
			Scan(&stored, &n); txErr != nil {
			return fmt.Errorf("count reads: %w", txErr)
		}
		if stored {
			return nil
		}
		if n >= domain.ReadLimit {
			return domain.ErrReadLimit
		}
		return insert(tx)
	})
	if err != nil {
		return fmt.Errorf("in %s: %w", table, err)
	}
	return nil
}

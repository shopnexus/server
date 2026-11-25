package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgAccounts struct{ db *pgxpool.Pool }

func NewAccounts(db *pgxpool.Pool) Accounts { return pgAccounts{db} }

const accountColumns = `account_id, carrier, shop_code, token, linked_at`

func scanAccount(row pgx.Row) (Row, error) {
	var r Row
	if err := row.Scan(&r.AccountID, &r.Carrier, &r.ShopCode, &r.Token, &r.LinkedAt); err != nil {
		return Row{}, fmt.Errorf("scan carrier account: %w", err)
	}
	return r, nil
}

// lockAccount is the lock module deal's save of a shipment takes too.
func lockAccount(b *pgx.Batch, owner uuid.UUID, carrier string) {
	postgres.QueueLock(b, "carrier_account", owner, carrier)
}

func (s pgAccounts) Put(ctx context.Context, r Row, open Guard) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// The shop read decides whether the guard runs, so the batch breaks.
		b := &pgx.Batch{}
		lockAccount(b, r.AccountID, r.Carrier)
		var shop *string
		b.Queue(`SELECT shop_code FROM carrier_accounts WHERE account_id = @owner AND carrier = @carrier`,
			pgx.StrictNamedArgs{"owner": r.AccountID, "carrier": r.Carrier}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&shop); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read carrier account: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // Each statement wraps its own error.
		}
		if shop != nil && *shop != r.ShopCode {
			if err := open(ctx); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO carrier_accounts (`+accountColumns+`)
VALUES (@account, @carrier, @shop, @token, @linked_at)
ON CONFLICT (account_id, carrier) DO UPDATE
SET shop_code = EXCLUDED.shop_code, token = EXCLUDED.token, linked_at = EXCLUDED.linked_at`,
			pgx.StrictNamedArgs{
				"account": r.AccountID, "carrier": r.Carrier, "shop": r.ShopCode, "token": r.Token, "linked_at": r.LinkedAt,
			}); err != nil {
			return fmt.Errorf("upsert carrier account: %w", err)
		}
		return nil
	})
	return err //nolint:wrapcheck // The callback wraps its own errors.
}

func (s pgAccounts) Get(ctx context.Context, owner uuid.UUID, carrier string) (Row, error) {
	r, err := scanAccount(s.db.QueryRow(ctx, `SELECT `+accountColumns+` FROM carrier_accounts
WHERE account_id = @owner AND carrier = @carrier`, pgx.StrictNamedArgs{"owner": owner, "carrier": carrier}))
	if errors.Is(err, pgx.ErrNoRows) {
		return Row{}, ErrNotFound
	}
	return r, err
}

func (s pgAccounts) List(ctx context.Context, owner uuid.UUID) ([]Row, error) {
	rows, err := s.db.Query(ctx, `SELECT `+accountColumns+` FROM carrier_accounts
WHERE account_id = @owner ORDER BY carrier`, pgx.StrictNamedArgs{"owner": owner})
	if err != nil {
		return nil, fmt.Errorf("select carrier accounts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Row, error) { return scanAccount(r) })
	if err != nil {
		return nil, fmt.Errorf("read carrier accounts: %w", err)
	}
	return out, nil
}

func (s pgAccounts) Delete(ctx context.Context, owner uuid.UUID, carrier string, open Guard) (bool, error) {
	var deleted bool
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		lockAccount(b, owner, carrier)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // QueueLock wraps its own error.
		}
		if err := open(ctx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM carrier_accounts WHERE account_id = @owner AND carrier = @carrier`,
			pgx.StrictNamedArgs{"owner": owner, "carrier": carrier})
		if err != nil {
			return fmt.Errorf("delete carrier account: %w", err)
		}
		deleted = tag.RowsAffected() > 0
		return nil
	})
	return deleted, err //nolint:wrapcheck // The callback wraps its own errors.
}

func (s pgAccounts) DeleteAll(ctx context.Context, owner uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM carrier_accounts WHERE account_id = @owner`, pgx.StrictNamedArgs{"owner": owner}); err != nil {
		return fmt.Errorf("delete carrier accounts: %w", err)
	}
	return nil
}

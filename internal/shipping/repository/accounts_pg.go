package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgAccounts struct{ db *pgxpool.Pool }

func NewAccounts(db *pgxpool.Pool) Accounts { return pgAccounts{db} }

const accountColumns = `account_id, carrier, shop_code, token, nonce, linked_at`

func scanAccount(row pgx.Row) (Row, error) {
	var r Row
	if err := row.Scan(&r.AccountID, &r.Carrier, &r.ShopCode, &r.Token, &r.Nonce, &r.LinkedAt); err != nil {
		return Row{}, fmt.Errorf("scan carrier account: %w", err)
	}
	return r, nil
}

func (s pgAccounts) Put(ctx context.Context, r Row) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO carrier_accounts (`+accountColumns+`)
VALUES (@account, @carrier, @shop, @token, @nonce, @linked_at)
ON CONFLICT (account_id, carrier) DO UPDATE
SET shop_code = EXCLUDED.shop_code, token = EXCLUDED.token, nonce = EXCLUDED.nonce, linked_at = EXCLUDED.linked_at`,
		pgx.StrictNamedArgs{
			"account": r.AccountID, "carrier": r.Carrier, "shop": r.ShopCode, "token": r.Token, "nonce": r.Nonce, "linked_at": r.LinkedAt,
		}); err != nil {
		return fmt.Errorf("upsert carrier account: %w", err)
	}
	return nil
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

func (s pgAccounts) Delete(ctx context.Context, owner uuid.UUID, carrier string) (bool, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM carrier_accounts WHERE account_id = @owner AND carrier = @carrier`,
		pgx.StrictNamedArgs{"owner": owner, "carrier": carrier})
	if err != nil {
		return false, fmt.Errorf("delete carrier account: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s pgAccounts) DeleteAll(ctx context.Context, owner uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM carrier_accounts WHERE account_id = @owner`, pgx.StrictNamedArgs{"owner": owner}); err != nil {
		return fmt.Errorf("delete carrier accounts: %w", err)
	}
	return nil
}

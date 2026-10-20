package account

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

type pgStore struct{}

// NewPGStore returns the Postgres implementation of Store.
func NewPGStore() Store { return pgStore{} }

const insertAccount = `
INSERT INTO accounts (display_name, status, created_at, updated_at)
VALUES (NULLIF($1, ''), $2, $3, $3)
RETURNING id, created_at`

func (pgStore) Insert(ctx context.Context, q postgres.Querier, a *Account) error {
	return q.QueryRow(ctx, insertAccount, a.displayName, a.status, a.createdAt).Scan(&a.id, &a.createdAt)
}

const getAccount = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), created_at, updated_at
FROM accounts WHERE id = $1`

func (pgStore) Get(ctx context.Context, q postgres.Querier, id uuid.UUID) (Account, error) {
	return scanAccount(ctx, q, getAccount, id)
}

func (pgStore) GetForUpdate(ctx context.Context, q postgres.Querier, id uuid.UUID) (Account, error) {
	return scanAccount(ctx, q, getAccount+" FOR UPDATE", id)
}

func scanAccount(ctx context.Context, q postgres.Querier, sql string, id uuid.UUID) (Account, error) {
	var a Account
	err := q.QueryRow(ctx, sql, id).Scan(
		&a.id, &a.displayName, &a.status, &a.suspendedUntil, &a.suspendReason, &a.createdAt, &a.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

const updateAccount = `
UPDATE accounts
SET display_name = NULLIF($2, ''), status = $3, suspended_until = $4, suspend_reason = NULLIF($5, ''), updated_at = $6
WHERE id = $1`

func (pgStore) Update(ctx context.Context, q postgres.Querier, a Account) error {
	tag, err := q.Exec(ctx, updateAccount, a.id, a.displayName, a.status, a.suspendedUntil, a.suspendReason, a.updatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

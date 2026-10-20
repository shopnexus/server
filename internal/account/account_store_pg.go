package account

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct{ db *pgxpool.Pool }

// NewPGStore returns the Postgres Store.
func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

const createAccount = `
INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
VALUES ($1, NULLIF($2, ''), $3, $4, $5, $5)
ON CONFLICT (id) DO NOTHING`

func (s pgStore) Create(ctx context.Context, a Account) error {
	_, err := s.db.Exec(ctx, createAccount, a.id, a.displayName, a.status, a.writeID, a.createdAt)
	return err
}

const getAccount = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), write_id, created_at, updated_at
FROM accounts WHERE id = $1`

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Account, error) {
	var a Account
	err := s.db.QueryRow(ctx, getAccount, id).Scan(&a.id, &a.displayName, &a.status, &a.suspendedUntil,
		&a.suspendReason, &a.writeID, &a.createdAt, &a.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

const saveAccount = `
UPDATE accounts
SET display_name = NULLIF($3, ''), status = $4, suspended_until = $5, suspend_reason = NULLIF($6, ''),
    updated_at = $7, write_id = $8
WHERE id = $1 AND (write_id = $2 OR write_id = $8)`

func (s pgStore) Save(ctx context.Context, a *Account, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveAccount, a.id, a.writeID, a.displayName, a.status, a.suspendedUntil,
		a.suspendReason, a.updatedAt, writeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	a.writeID = writeID
	return nil
}

func (s pgStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	return err
}

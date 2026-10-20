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
VALUES (@id, NULLIF(@display_name, ''), @status, @write_id, @now, @now)
ON CONFLICT (id) DO NOTHING`

func (s pgStore) Create(ctx context.Context, a Account) error {
	_, err := s.db.Exec(ctx, createAccount, pgx.StrictNamedArgs{
		"id": a.id, "display_name": a.displayName, "status": a.status, "write_id": a.writeID, "now": a.createdAt,
	})
	return err
}

const getAccount = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), write_id, created_at, updated_at
FROM accounts WHERE id = @id`

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Account, error) {
	var a Account
	err := s.db.QueryRow(ctx, getAccount, pgx.StrictNamedArgs{"id": id}).Scan(&a.id, &a.displayName, &a.status, &a.suspendedUntil,
		&a.suspendReason, &a.writeID, &a.createdAt, &a.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

const saveAccount = `
UPDATE accounts
SET display_name = NULLIF(@display_name, ''), status = @status, suspended_until = @suspended_until,
    suspend_reason = NULLIF(@suspend_reason, ''), updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgStore) Save(ctx context.Context, a *Account, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
		"id": a.id, "read_write_id": a.writeID, "write_id": writeID,
		"display_name": a.displayName, "status": a.status, "suspended_until": a.suspendedUntil,
		"suspend_reason": a.suspendReason, "updated_at": a.updatedAt,
	})
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
	_, err := s.db.Exec(ctx, `DELETE FROM accounts WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	return err
}

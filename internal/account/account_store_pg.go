package account

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
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
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
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
	if err != nil {
		return Account{}, fmt.Errorf("select account: %w", err)
	}
	return a, nil
}

const saveAccount = `
UPDATE accounts
SET display_name = NULLIF(@display_name, ''), status = @status, suspended_until = @suspended_until,
    suspend_reason = NULLIF(@suspend_reason, ''), updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

// execer is a pool or a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func update(ctx context.Context, q execer, a *Account, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
		"id": a.id, "read_write_id": a.writeID, "write_id": writeID,
		"display_name": a.displayName, "status": a.status, "suspended_until": a.suspendedUntil,
		"suspend_reason": a.suspendReason, "updated_at": a.updatedAt,
	})
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgStore) Save(ctx context.Context, a *Account, writeID uuid.UUID) error {
	if err := update(ctx, s.db, a, writeID); err != nil {
		return err
	}
	a.writeID = writeID
	return nil
}

func (s pgStore) SaveAudited(ctx context.Context, a *Account, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := update(ctx, tx, a, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save account with audit: %w", err)
	}
	a.writeID = writeID
	return nil
}

const listAccounts = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), write_id, created_at, updated_at
FROM accounts WHERE id < @before ORDER BY id DESC LIMIT @limit`

func (s pgStore) List(ctx context.Context, p page.Request) ([]Account, error) {
	rows, err := s.db.Query(ctx, listAccounts, pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.id, &a.displayName, &a.status, &a.suspendedUntil, &a.suspendReason, &a.writeID,
			&a.createdAt, &a.updatedAt); err != nil {
			return nil, fmt.Errorf("read account: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	return out, nil
}

func (s pgStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM accounts WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

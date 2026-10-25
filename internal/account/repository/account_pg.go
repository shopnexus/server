package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgAccounts struct{ db *pgxpool.Pool }

// NewAccounts returns the Postgres Accounts.
func NewAccounts(db *pgxpool.Pool) Accounts { return pgAccounts{db} }

const createAccount = `
INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
VALUES (@id, NULLIF(@display_name, ''), @status, @write_id, @now, @now)
ON CONFLICT (id) DO NOTHING`

func (s pgAccounts) Create(ctx context.Context, a domain.Account) error {
	_, err := s.db.Exec(ctx, createAccount, pgx.StrictNamedArgs{
		"id": a.ID(), "display_name": a.DisplayName(), "status": a.StoredStatus(), "write_id": a.WriteID(), "now": a.CreatedAt(),
	})
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

const getAccount = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), write_id, created_at, updated_at
FROM accounts WHERE id = @id`

// scanAccount reads the columns of getAccount and listAccounts.
func scanAccount(row pgx.Row) (domain.Account, error) {
	var (
		id, writeID          uuid.UUID
		name, reason         string
		status               domain.Status
		until                *time.Time
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&id, &name, &status, &until, &reason, &writeID, &createdAt, &updatedAt); err != nil {
		return domain.Account{}, fmt.Errorf("scan account: %w", err)
	}
	return domain.RestoreAccount(id, name, status, until, reason, writeID, createdAt, updatedAt), nil
}

func (s pgAccounts) Get(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	a, err := scanAccount(s.db.QueryRow(ctx, getAccount, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("select account: %w", err)
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

func update(ctx context.Context, q execer, a *domain.Account, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
		"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
		"display_name": a.DisplayName(), "status": a.StoredStatus(), "suspended_until": a.SuspendedUntil(),
		"suspend_reason": a.SuspendReason(), "updated_at": a.UpdatedAt(),
	})
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgAccounts) Save(ctx context.Context, a *domain.Account, writeID uuid.UUID) error {
	if err := update(ctx, s.db, a, writeID); err != nil {
		return err
	}
	a.Saved(writeID)
	return nil
}

func (s pgAccounts) SaveAudited(ctx context.Context, a *domain.Account, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := update(ctx, tx, a, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save account with audit: %w", err)
	}
	a.Saved(writeID)
	return nil
}

const listAccounts = `
SELECT id, coalesce(display_name, ''), status, suspended_until, coalesce(suspend_reason, ''), write_id, created_at, updated_at
FROM accounts WHERE id < @before ORDER BY id DESC LIMIT @limit`

func (s pgAccounts) List(ctx context.Context, p page.Request) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, listAccounts, pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("read account: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	return out, nil
}

func (s pgAccounts) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM accounts WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

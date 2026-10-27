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
	"github.com/shopnexus/server2/internal/platform/events"
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

// accountColumns are the columns scanAccount reads.
const accountColumns = `id, coalesce(display_name, ''), coalesce(bio, ''), coalesce(avatar_key, ''), status, suspended_until,
       coalesce(suspend_reason, ''), write_id, created_at, updated_at`

const getAccount = `SELECT ` + accountColumns + ` FROM accounts WHERE id = @id`

// scanAccount reads accountColumns.
func scanAccount(row pgx.Row) (domain.Account, error) {
	var (
		id, writeID                  uuid.UUID
		name, bio, avatarKey, reason string
		status                       domain.Status
		until                        *time.Time
		createdAt, updatedAt         time.Time
	)
	if err := row.Scan(&id, &name, &bio, &avatarKey, &status, &until, &reason, &writeID, &createdAt, &updatedAt); err != nil {
		return domain.Account{}, fmt.Errorf("scan account: %w", err)
	}
	return domain.RestoreAccount(id, name, bio, avatarKey, status, until, reason, writeID, createdAt, updatedAt), nil
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
SET display_name = NULLIF(@display_name, ''), bio = NULLIF(@bio, ''), avatar_key = NULLIF(@avatar_key, ''), status = @status, suspended_until = @suspended_until,
    suspend_reason = NULLIF(@suspend_reason, ''), updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

// execer is a pool or a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func update(ctx context.Context, q execer, a *domain.Account, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
		"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
		"display_name": a.DisplayName(), "bio": a.Bio(), "avatar_key": a.AvatarKey(), "status": a.StoredStatus(), "suspended_until": a.SuspendedUntil(),
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

func (s pgAccounts) Save(ctx context.Context, a *domain.Account, writeID uuid.UUID, evs ...events.Event) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := update(ctx, tx, a, writeID); err != nil {
			return err
		}
		return insertEvents(ctx, tx, evs)
	})
	if err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	a.Saved(writeID)
	return nil
}

func (s pgAccounts) SaveAudited(ctx context.Context, a *domain.Account, writeID uuid.UUID, e audit.Entry, evs ...events.Event) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := update(ctx, tx, a, writeID); err != nil {
			return err
		}
		if err := audit.Insert(ctx, tx, e); err != nil {
			return err
		}
		return insertEvents(ctx, tx, evs)
	})
	if err != nil {
		return fmt.Errorf("save account with audit: %w", err)
	}
	a.Saved(writeID)
	return nil
}

// insertEvents writes evs in tx, the transaction of the change they
// announce.
func insertEvents(ctx context.Context, tx pgx.Tx, evs []events.Event) error {
	for _, e := range evs {
		if err := events.Insert(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}

func (s pgAccounts) GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	return collect(rows)
}

const listAccounts = `SELECT ` + accountColumns + ` FROM accounts WHERE id < @before ORDER BY id DESC LIMIT @limit`

func (s pgAccounts) List(ctx context.Context, p page.Request) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, listAccounts, pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	return collect(rows)
}

// collect scans every row of an accounts query.
func collect(rows pgx.Rows) ([]domain.Account, error) {
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

func (s pgAccounts) Block(ctx context.Context, id, blocker, blocked uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO account_blocks (id, blocker_id, blocked_id, created_at)
VALUES (@id, @blocker, @blocked, @at) ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
		pgx.StrictNamedArgs{"id": id, "blocker": blocker, "blocked": blocked, "at": at}); err != nil {
		return fmt.Errorf("insert block: %w", err)
	}
	return nil
}

func (s pgAccounts) Unblock(ctx context.Context, blocker, blocked uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_id = @blocker AND blocked_id = @blocked`,
		pgx.StrictNamedArgs{"blocker": blocker, "blocked": blocked}); err != nil {
		return fmt.Errorf("delete block: %w", err)
	}
	return nil
}

func (s pgAccounts) Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var blocked bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM account_blocks
WHERE (blocker_id = @a AND blocked_id = @b) OR (blocker_id = @b AND blocked_id = @a))`,
		pgx.StrictNamedArgs{"a": a, "b": b}).Scan(&blocked); err != nil {
		return false, fmt.Errorf("check block: %w", err)
	}
	return blocked, nil
}

func (s pgAccounts) ListBlocks(ctx context.Context, blocker uuid.UUID, p page.Request) ([]Block, error) {
	rows, err := s.db.Query(ctx, `SELECT id, blocked_id, created_at FROM account_blocks
WHERE blocker_id = @blocker AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"blocker": blocker, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select blocks: %w", err)
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		if err = rows.Scan(&b.ID, &b.AccountID, &b.At); err != nil {
			return nil, fmt.Errorf("scan block: %w", err)
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	return out, nil
}

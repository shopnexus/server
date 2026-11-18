package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgAccounts struct{ db *pgxpool.Pool }

// NewAccounts returns the Postgres Accounts.
func NewAccounts(db *pgxpool.Pool) Accounts { return pgAccounts{db} }

func (s pgAccounts) Create(ctx context.Context, a domain.Account) error {
	return insertAccount(ctx, s.db, a)
}

// insertAccount inserts a; the same id again does nothing. Create and
// Credentials.SignUp call it.
func insertAccount(ctx context.Context, q postgres.Execer, a domain.Account) error {
	_, err := q.Exec(ctx, `
INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
VALUES (@id, NULLIF(@display_name, ''), @status, @write_id, @now, @now)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
		"id": a.ID(), "display_name": a.DisplayName(), "status": a.StoredStatus(), "write_id": a.WriteID(), "now": a.CreatedAt(),
	})
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

// accountColumns are the columns scanAccount reads.
const accountColumns = `id, coalesce(display_name, ''), coalesce(bio, ''), coalesce(avatar_key, ''), status, suspended_until,
       coalesce(suspend_reason, ''), write_id, created_at, updated_at, last_active_at, deleted_at`

// scanAccount reads accountColumns.
func scanAccount(row pgx.CollectableRow) (domain.Account, error) {
	var (
		id, writeID                  uuid.UUID
		name, bio, avatarKey, reason string
		status                       domain.Status
		until, lastActive, deletedAt *time.Time
		createdAt, updatedAt         time.Time
	)
	if err := row.Scan(&id, &name, &bio, &avatarKey, &status, &until, &reason, &writeID, &createdAt, &updatedAt, &lastActive, &deletedAt); err != nil {
		return domain.Account{}, fmt.Errorf("scan account: %w", err)
	}
	return domain.RestoreAccount(id, name, bio, avatarKey, status, until, reason, writeID, createdAt, updatedAt, lastActive, deletedAt), nil
}

func (s pgAccounts) Get(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	rows, err := s.db.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return domain.Account{}, fmt.Errorf("select account: %w", err)
	}
	a, err := pgx.CollectExactlyOneRow(rows, scanAccount)
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
    suspend_reason = NULLIF(@suspend_reason, ''), updated_at = @updated_at, deleted_at = @deleted_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgAccounts) Save(ctx context.Context, a *domain.Account, writeID uuid.UUID, entry *audit.Entry, evs ...events.Event) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
			"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
			"display_name": a.DisplayName(), "bio": a.Bio(), "avatar_key": a.AvatarKey(), "status": a.StoredStatus(),
			"suspended_until": a.SuspendedUntil(), "suspend_reason": a.SuspendReason(), "updated_at": a.UpdatedAt(),
			"deleted_at": a.DeletedAt(),
		})
		if err != nil {
			return fmt.Errorf("update account: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		if entry != nil {
			if err := audit.Insert(ctx, tx, *entry); err != nil {
				return err
			}
		}
		return events.Insert(ctx, tx, evs...)
	})
	if err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	a.Saved(writeID)
	return nil
}

// eraseAccount is what Erase deletes beside the row, in order; identities
// go with their credentials (ON DELETE CASCADE). The email codes go first,
// while the identities still name the account's address.
var eraseAccount = []string{
	`DELETE FROM otp_challenges WHERE account_id = @id
OR destination IN (SELECT identifier FROM identities WHERE account_id = @id AND kind = 'email')`,
	`DELETE FROM otp_quotas
WHERE subject IN (SELECT 'dest:' || identifier FROM identities WHERE account_id = @id AND kind = 'email')`,
	`DELETE FROM credentials WHERE account_id = @id`,
	`DELETE FROM addresses WHERE account_id = @id`,
	`DELETE FROM account_blocks WHERE blocker_id = @id OR blocked_id = @id`,
	`DELETE FROM account_follows WHERE follower_id = @id OR followed_id = @id`,
}

func (s pgAccounts) Erase(ctx context.Context, a *domain.Account, writeID uuid.UUID, evs ...events.Event) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, saveAccount, pgx.StrictNamedArgs{
			"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
			"display_name": a.DisplayName(), "bio": a.Bio(), "avatar_key": a.AvatarKey(), "status": a.StoredStatus(),
			"suspended_until": a.SuspendedUntil(), "suspend_reason": a.SuspendReason(), "updated_at": a.UpdatedAt(),
			"deleted_at": a.DeletedAt(),
		})
		if err != nil {
			return fmt.Errorf("update account: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		for _, q := range eraseAccount {
			if _, err = tx.Exec(ctx, q, pgx.StrictNamedArgs{"id": a.ID()}); err != nil {
				return fmt.Errorf("erase: %w", err)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = @now, revoke_reason = @reason, write_id = uuidv7()
WHERE account_id = @id AND revoked_at IS NULL`,
			pgx.StrictNamedArgs{"id": a.ID(), "now": a.UpdatedAt(), "reason": string(domain.RevokedDeleted)}); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return events.Insert(ctx, tx, evs...)
	})
	if err != nil {
		return fmt.Errorf("erase account: %w", err)
	}
	a.Saved(writeID)
	return nil
}

func (s pgAccounts) GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	accounts, err := pgx.CollectRows(rows, scanAccount)
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	return accounts, nil
}

// listAccounts leaves out the ShopNexus system account, which is no
// customer's; Get still reads it.
const listAccounts = `SELECT ` + accountColumns + ` FROM accounts WHERE id < @before AND id <> @system
ORDER BY id DESC LIMIT @limit`

func (s pgAccounts) List(ctx context.Context, p page.Request) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, listAccounts, pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch(), "system": domain.ShopNexusID})
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	accounts, err := pgx.CollectRows(rows, scanAccount)
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	return accounts, nil
}

// suspendedNow are the accounts whose suspension is in force at @now: a
// lapsed one reads as active, as domain.Account.Status says.
const suspendedNow = `status = 'suspended' AND (suspended_until IS NULL OR suspended_until > @now) AND id <> @system`

// listSuspended orders by the suspension's end, a missing end as infinity,
// then by ID. The cursor is the ID of the last account listed; the query
// reads that account's end to know where to go on. On the first page the
// cursor is the zero ID and @first is true.
const listSuspended = `SELECT ` + accountColumns + ` FROM accounts WHERE ` + suspendedNow + `
  AND (@first OR (coalesce(suspended_until, 'infinity'), id) >
      (SELECT coalesce(suspended_until, 'infinity'), id FROM accounts WHERE id = @cursor))
ORDER BY coalesce(suspended_until, 'infinity'), id LIMIT @limit`

func (s pgAccounts) ListSuspended(ctx context.Context, now time.Time, p page.Request) ([]domain.Account, error) {
	rows, err := s.db.Query(ctx, listSuspended, pgx.StrictNamedArgs{
		"now": now, "system": domain.ShopNexusID, "first": p.Cursor == (uuid.UUID{}), "cursor": p.Cursor, "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select suspended accounts: %w", err)
	}
	accounts, err := pgx.CollectRows(rows, scanAccount)
	if err != nil {
		return nil, fmt.Errorf("read suspended accounts: %w", err)
	}
	return accounts, nil
}

func (s pgAccounts) CountSuspended(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE `+suspendedNow, pgx.StrictNamedArgs{"now": now, "system": domain.ShopNexusID}).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count suspended accounts: %w", err)
	}
	return n, nil
}

func (s pgAccounts) CountByStatus(ctx context.Context, now, since time.Time) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT CASE WHEN status = 'deleted' THEN 'deleted'
    WHEN status = 'suspended' AND (suspended_until IS NULL OR suspended_until > @now) THEN 'suspended'
    ELSE 'active' END, count(*)
FROM accounts WHERE id <> @system AND created_at >= @since GROUP BY 1`,
		pgx.StrictNamedArgs{"now": now, "since": since, "system": domain.ShopNexusID}))
}

// lockLiving takes a share lock on account's row and refuses a deleted
// account with domain.ErrSessionRevoked. A write that checked the account
// before Erase and commits after it waits for Erase and then sees the
// tombstone, so no new row of the account outlives the deletion. An unknown
// account passes; the foreign keys answer for it.
func lockLiving(ctx context.Context, tx pgx.Tx, account uuid.UUID) error {
	var status domain.Status
	err := tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id = @id FOR SHARE`, pgx.StrictNamedArgs{"id": account}).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock account: %w", err)
	}
	if status == domain.StatusDeleted {
		return domain.ErrSessionRevoked
	}
	return nil
}

// lockPair serialises the follows and blocks between a and b, in either
// order, so a follow written beside a block cannot outlive it.
func lockPair(ctx context.Context, tx pgx.Tx, a, b uuid.UUID) error {
	lo, hi := min(a.String(), b.String()), max(a.String(), b.String())
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('pair:' || @pair))`,
		pgx.StrictNamedArgs{"pair": lo + "/" + hi}); err != nil {
		return fmt.Errorf("lock pair: %w", err)
	}
	return nil
}

func (s pgAccounts) Block(ctx context.Context, id, blocker, blocked uuid.UUID, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockLiving(ctx, tx, blocker); err != nil {
			return err
		}
		if err := lockPair(ctx, tx, blocker, blocked); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_blocks (id, blocker_id, blocked_id, created_at)
VALUES (@id, @blocker, @blocked, @at) ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
			pgx.StrictNamedArgs{"id": id, "blocker": blocker, "blocked": blocked, "at": at}); err != nil {
			return fmt.Errorf("insert block: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM account_follows
WHERE (follower_id = @blocker AND followed_id = @blocked) OR (follower_id = @blocked AND followed_id = @blocker)`,
			pgx.StrictNamedArgs{"blocker": blocker, "blocked": blocked}); err != nil {
			return fmt.Errorf("delete follows: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("block: %w", err)
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

func (s pgAccounts) BlockedWith(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT blocked_id FROM account_blocks WHERE blocker_id = @a
UNION SELECT blocker_id FROM account_blocks WHERE blocked_id = @a`, pgx.StrictNamedArgs{"a": account})
	if err != nil {
		return nil, fmt.Errorf("select blocks: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	return ids, nil
}

func (s pgAccounts) ListBlocks(ctx context.Context, blocker uuid.UUID, p page.Request) ([]Block, error) {
	rows, err := s.db.Query(ctx, `SELECT id, blocked_id, created_at FROM account_blocks
WHERE blocker_id = @blocker AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"blocker": blocker, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select blocks: %w", err)
	}
	blocks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Block])
	if err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	return blocks, nil
}

func (s pgAccounts) TouchActive(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `
UPDATE accounts SET last_active_at = @at
WHERE id = @id AND (last_active_at IS NULL OR last_active_at < @at::timestamptz - interval '30 seconds')`,
		pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
		return fmt.Errorf("touch account: %w", err)
	}
	return nil
}

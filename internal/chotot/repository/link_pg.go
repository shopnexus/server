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

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgCodes struct{ db *pgxpool.Pool }

func NewCodes(db *pgxpool.Pool) Codes { return pgCodes{db} }

func (s pgCodes) Issue(ctx context.Context, account uuid.UUID, c domain.Code, now time.Time) (domain.Code, error) {
	var out domain.Code
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
		b.Queue(`INSERT INTO chotot_codes (account_id, code, expires_at) VALUES (@account, @code, @expires)
ON CONFLICT (account_id) DO UPDATE SET code = excluded.code, expires_at = excluded.expires_at
    WHERE chotot_codes.expires_at <= @now`,
			pgx.StrictNamedArgs{"account": account, "code": c.Code, "expires": c.ExpiresAt, "now": now}).Exec(func(pgconn.CommandTag) error {
			return nil
		})
		b.Queue(`SELECT code, expires_at FROM chotot_codes WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": account}).QueryRow(func(row pgx.Row) error {
			return row.Scan(&out.Code, &out.ExpiresAt)
		})
		return tx.SendBatch(ctx, b).Close()
	})
	if errors.Is(err, accountdomain.ErrSessionRevoked) {
		return domain.Code{}, err //nolint:wrapcheck // a refusal
	}
	if err != nil {
		return domain.Code{}, fmt.Errorf("issue code: %w", err)
	}
	return out, nil
}

func (s pgCodes) Get(ctx context.Context, account uuid.UUID) (domain.Code, bool, error) {
	var c domain.Code
	err := s.db.QueryRow(ctx, `SELECT code, expires_at FROM chotot_codes WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}).Scan(&c.Code, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Code{}, false, nil
	}
	if err != nil {
		return domain.Code{}, false, fmt.Errorf("read code: %w", err)
	}
	return c, true, nil
}

type pgLinks struct{ db *pgxpool.Pool }

func NewLinks(db *pgxpool.Pool) Links { return pgLinks{db} }

// linkColumns are a Link's columns in its field order.
const linkColumns = `id, account_id, oid, chotot_account, display_name, avatar_url, auto_import, follow_updates,
remind_hide, seen_through, waiting, linked_at, checked_at`

func scanLink(row pgx.Row) (domain.Link, error) {
	var l domain.Link
	err := row.Scan(&l.ID, &l.AccountID, &l.OID, &l.ChototAccount, &l.Name, &l.AvatarURL, &l.AutoImport, &l.FollowUpdates,
		&l.RemindHide, &l.SeenThrough, &l.Waiting, &l.LinkedAt, &l.CheckedAt)
	return l, err //nolint:wrapcheck // the caller wraps
}

// errStaleCode stops Link's batch when the account's code changed.
var errStaleCode = errors.New("chotot: the code changed")

func (s pgLinks) Link(ctx context.Context, l domain.Link, code string) (domain.Link, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, l.AccountID, accountdomain.ErrSessionRevoked)
		// The profile first, then the account: two links of one profile
		// wait for each other, and two of one account too.
		postgres.QueueLock(b, "chotot_link_oid", l.OID)
		postgres.QueueLock(b, "chotot_link_account", l.AccountID)
		b.Queue(`DELETE FROM chotot_codes WHERE account_id = @account AND code = @code`,
			pgx.StrictNamedArgs{"account": l.AccountID, "code": code}).Exec(func(tag pgconn.CommandTag) error {
			if tag.RowsAffected() == 0 {
				return errStaleCode
			}
			return nil
		})
		b.Queue(`SELECT auto_import, follow_updates, remind_hide FROM chotot_links WHERE account_id = @account AND oid = @oid`,
			pgx.StrictNamedArgs{"account": l.AccountID, "oid": l.OID}).Query(func(rows pgx.Rows) error {
			for rows.Next() {
				if err := rows.Scan(&l.AutoImport, &l.FollowUpdates, &l.RemindHide); err != nil {
					return fmt.Errorf("read settings: %w", err)
				}
			}
			return rows.Err()
		})
		b.Queue(`DELETE FROM chotot_links WHERE account_id = @account OR oid = @oid`,
			pgx.StrictNamedArgs{"account": l.AccountID, "oid": l.OID}).Exec(func(pgconn.CommandTag) error { return nil })
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		_, err := tx.Exec(ctx, `INSERT INTO chotot_links (`+linkColumns+`)
VALUES (@id, @account, @oid, @chotot_account, @name, @avatar, @auto_import, @follow_updates, @remind_hide,
    @seen_through, @waiting, @linked_at, NULL)`, pgx.StrictNamedArgs{
			"id": l.ID, "account": l.AccountID, "oid": l.OID, "chotot_account": l.ChototAccount, "name": l.Name,
			"avatar": l.AvatarURL, "auto_import": l.AutoImport, "follow_updates": l.FollowUpdates,
			"remind_hide": l.RemindHide, "seen_through": l.SeenThrough, "waiting": l.Waiting, "linked_at": l.LinkedAt,
		})
		return err //nolint:wrapcheck // wrapped below
	})
	switch {
	case errors.Is(err, errStaleCode):
		return domain.Link{}, domain.ErrCodeExpired
	case errors.Is(err, accountdomain.ErrSessionRevoked):
		return domain.Link{}, err //nolint:wrapcheck // a refusal
	case err != nil:
		return domain.Link{}, fmt.Errorf("link profile: %w", err)
	}
	l.CheckedAt = nil
	return l, nil
}

func (s pgLinks) one(ctx context.Context, where string, arg any) (domain.Link, bool, error) {
	l, err := scanLink(s.db.QueryRow(ctx, `SELECT `+linkColumns+` FROM chotot_links WHERE `+where+` = @arg`,
		pgx.StrictNamedArgs{"arg": arg}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Link{}, false, nil
	}
	if err != nil {
		return domain.Link{}, false, fmt.Errorf("read link: %w", err)
	}
	return l, true, nil
}

func (s pgLinks) Get(ctx context.Context, account uuid.UUID) (domain.Link, bool, error) {
	return s.one(ctx, "account_id", account)
}

func (s pgLinks) ByID(ctx context.Context, id uuid.UUID) (domain.Link, bool, error) {
	return s.one(ctx, "id", id)
}

func (s pgLinks) Update(ctx context.Context, account uuid.UUID, p domain.SettingsPatch) (domain.Link, bool, error) {
	l, err := scanLink(s.db.QueryRow(ctx, `UPDATE chotot_links SET
    auto_import = coalesce(@auto_import, auto_import), follow_updates = coalesce(@follow_updates, follow_updates),
    remind_hide = coalesce(@remind_hide, remind_hide)
WHERE account_id = @account RETURNING `+linkColumns, pgx.StrictNamedArgs{
		"account": account, "auto_import": p.AutoImport, "follow_updates": p.FollowUpdates, "remind_hide": p.RemindHide,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Link{}, false, nil
	}
	if err != nil {
		return domain.Link{}, false, fmt.Errorf("update link: %w", err)
	}
	return l, true, nil
}

func (s pgLinks) Checked(ctx context.Context, id uuid.UUID, at time.Time, waiting int, seenThrough int64) error {
	if _, err := s.db.Exec(ctx, `UPDATE chotot_links SET checked_at = @at, waiting = @waiting,
    seen_through = greatest(seen_through, @seen) WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id, "at": at, "waiting": waiting, "seen": seenThrough}); err != nil {
		return fmt.Errorf("record check: %w", err)
	}
	return nil
}

func (s pgLinks) Imported(ctx context.Context, id uuid.UUID, count int, event uuid.UUID, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM chotot_links WHERE id = @id FOR SHARE)`,
			pgx.StrictNamedArgs{"id": id}).Scan(&found); err != nil || !found {
			return err //nolint:wrapcheck // wrapped below
		}
		b := &pgx.Batch{}
		events.Queue(b, events.Event{
			ID: event, Kind: events.ChototLinkImported, SubjectID: id, Payload: map[string]any{"count": count}, OccurredAt: at,
		})
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("announce import: %w", err)
	}
	return nil
}

func (s pgLinks) Delete(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM chotot_links WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("unlink: %w", err)
	}
	return nil
}

func (s pgLinks) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	b := &pgx.Batch{}
	for _, table := range []string{"chotot_codes", "chotot_links", "chotot_imports"} {
		b.Queue(`DELETE FROM `+table+` WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}).
			Exec(func(pgconn.CommandTag) error { return nil })
	}
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return tx.SendBatch(ctx, b).Close() }); err != nil {
		return fmt.Errorf("delete the account's links: %w", err)
	}
	return nil
}

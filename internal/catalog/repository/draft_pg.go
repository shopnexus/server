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
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgDrafts struct{ db *pgxpool.Pool }

func NewDrafts(db *pgxpool.Pool) Drafts { return pgDrafts{db} }

const draftColumns = `id, account_id, schema_version, title, price, photos, body, created_at, updated_at, expires_at`

func scanDraft(row pgx.CollectableRow) (domain.ListingDraft, error) {
	var st domain.ListingDraftState
	if err := row.Scan(&st.ID, &st.AccountID, &st.Content.SchemaVersion, &st.Content.Title, &st.Content.Price,
		&st.Content.Photos, &st.Content.Body, &st.CreatedAt, &st.UpdatedAt, &st.ExpiresAt); err != nil {
		return domain.ListingDraft{}, fmt.Errorf("scan draft: %w", err)
	}
	return domain.RestoreListingDraft(st), nil
}

// deleteExpired deletes account's drafts that expired by now. Put and List
// queue it first.
func deleteExpired(b *pgx.Batch, account uuid.UUID, now time.Time) {
	b.Queue(`DELETE FROM listing_drafts WHERE account_id = @account AND expires_at <= @now`,
		pgx.StrictNamedArgs{"account": account, "now": now})
}

func (s pgDrafts) Put(ctx context.Context, account, id uuid.UUID, c domain.DraftContent, now time.Time) (domain.ListingDraft, bool, error) {
	var out domain.ListingDraft
	var created bool
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			stored *domain.ListingDraft
			count  int
		)
		b := &pgx.Batch{}
		postgres.QueueLock(b, "drafts", account)
		// A write that commits after Erase waits for it here and then finds
		// the tombstone.
		postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
		deleteExpired(b, account, now)
		b.Queue(`SELECT `+draftColumns+` FROM listing_drafts WHERE id = @id FOR UPDATE`,
			pgx.StrictNamedArgs{"id": id}).Query(func(rows pgx.Rows) error {
			found, err := pgx.CollectRows(rows, scanDraft)
			if err != nil {
				return fmt.Errorf("read draft: %w", err)
			}
			if len(found) == 1 {
				stored = &found[0]
			}
			return nil
		})
		b.Queue(`SELECT count(*) FROM listing_drafts WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": account}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&count); err != nil {
				return fmt.Errorf("count drafts: %w", err)
			}
			return nil
		})
		err := tx.SendBatch(ctx, b).Close()
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if err = domain.CheckPut(stored, account, count); err != nil {
			return err
		}
		if stored == nil {
			out, created = domain.NewListingDraft(id, account, c, now), true
		} else {
			out = *stored
			out.Replace(c, now)
		}
		st := out.State()
		a := pgx.StrictNamedArgs{
			"id": st.ID, "account": st.AccountID, "schema_version": st.Content.SchemaVersion, "title": st.Content.Title,
			"price": st.Content.Price, "photos": st.Content.Photos, "body": st.Content.Body,
			"updated_at": st.UpdatedAt, "expires_at": st.ExpiresAt,
		}
		if created {
			a["created_at"] = st.CreatedAt
			// Another account's create of the same ID is the only race left,
			// since the lock is per account: the loser sees it as theirs.
			var tag pgconn.CommandTag
			tag, err = tx.Exec(ctx, `INSERT INTO listing_drafts (`+draftColumns+`)
VALUES (@id, @account, @schema_version, @title, @price, @photos, @body, @created_at, @updated_at, @expires_at)
ON CONFLICT (id) DO NOTHING`, a)
			if err != nil {
				return fmt.Errorf("insert draft: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return domain.ErrDraftNotFound
			}
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE listing_drafts
SET schema_version = @schema_version, title = @title, price = @price, photos = @photos, body = @body,
    updated_at = @updated_at, expires_at = @expires_at
WHERE id = @id AND account_id = @account`, a); err != nil {
			return fmt.Errorf("update draft: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.ListingDraft{}, false, fmt.Errorf("put draft: %w", err)
	}
	return out, created, nil
}

func (s pgDrafts) List(ctx context.Context, account uuid.UUID, now time.Time) ([]domain.ListingDraft, error) {
	var out []domain.ListingDraft
	// One implicit transaction on the pool: nothing in it aborts.
	b := &pgx.Batch{}
	deleteExpired(b, account, now)
	b.Queue(`SELECT `+draftColumns+` FROM listing_drafts WHERE account_id = @account
ORDER BY updated_at DESC, id DESC LIMIT @limit`, pgx.StrictNamedArgs{"account": account, "limit": domain.MaxDrafts}).
		Query(func(rows pgx.Rows) error {
			var err error
			if out, err = pgx.CollectRows(rows, scanDraft); err != nil {
				return fmt.Errorf("read drafts: %w", err)
			}
			return nil
		})
	err := s.db.SendBatch(ctx, b).Close()
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	return out, nil
}

func (s pgDrafts) Get(ctx context.Context, account, id uuid.UUID, now time.Time) (domain.ListingDraft, error) {
	rows, err := s.db.Query(ctx, `SELECT `+draftColumns+` FROM listing_drafts
WHERE id = @id AND account_id = @account AND expires_at > @now`, pgx.StrictNamedArgs{"id": id, "account": account, "now": now})
	if err != nil {
		return domain.ListingDraft{}, fmt.Errorf("select draft: %w", err)
	}
	d, err := pgx.CollectOneRow(rows, scanDraft)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ListingDraft{}, domain.ErrDraftNotFound
	}
	if err != nil {
		return domain.ListingDraft{}, fmt.Errorf("read draft: %w", err)
	}
	return d, nil
}

func (s pgDrafts) Delete(ctx context.Context, account, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listing_drafts WHERE id = @id AND account_id = @account`,
		pgx.StrictNamedArgs{"id": id, "account": account}); err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}
	return nil
}

func (s pgDrafts) DeleteAll(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listing_drafts WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete drafts: %w", err)
	}
	return nil
}

func (s pgDrafts) CountLive(ctx context.Context, now time.Time) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM listing_drafts WHERE expires_at > @now`, pgx.StrictNamedArgs{"now": now}).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("count drafts: %w", err)
	}
	return n, nil
}

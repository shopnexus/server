package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgSaves struct{ db *pgxpool.Pool }

func NewSaves(db *pgxpool.Pool) Saves { return pgSaves{db} }

func (s pgSaves) Save(ctx context.Context, id, account, listing uuid.UUID, price int64, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@account::text))`,
			pgx.StrictNamedArgs{"account": account}); err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		var (
			saved bool
			n     int
		)
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM saved_listings WHERE account_id = @account AND listing_id = @listing),
       (SELECT count(*) FROM saved_listings WHERE account_id = @account)`,
			pgx.StrictNamedArgs{"account": account, "listing": listing}).Scan(&saved, &n); err != nil {
			return fmt.Errorf("count saves: %w", err)
		}
		if saved {
			return nil
		}
		if n >= domain.MaxSaved {
			return domain.ErrLimit
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saved_listings (id, account_id, listing_id, saved_price, created_at)
VALUES (@id, @account, @listing, @price, @at)`, pgx.StrictNamedArgs{
			"id": id, "account": account, "listing": listing, "price": price, "at": at,
		}); err != nil {
			return fmt.Errorf("insert save: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save listing: %w", err)
	}
	return nil
}

func (s pgSaves) Remove(ctx context.Context, account, listing uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM saved_listings WHERE account_id = @account AND listing_id = @listing`,
		pgx.StrictNamedArgs{"account": account, "listing": listing}); err != nil {
		return fmt.Errorf("remove save: %w", err)
	}
	return nil
}

func (s pgSaves) Has(ctx context.Context, account, listing uuid.UUID) (bool, error) {
	var has bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM saved_listings WHERE account_id = @account AND listing_id = @listing)`,
		pgx.StrictNamedArgs{"account": account, "listing": listing}).Scan(&has); err != nil {
		return false, fmt.Errorf("select save: %w", err)
	}
	return has, nil
}

func (s pgSaves) List(ctx context.Context, account uuid.UUID, p page.Request) ([]Save, error) {
	rows, err := s.db.Query(ctx, `SELECT id, listing_id, saved_price, created_at FROM saved_listings
WHERE account_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select saves: %w", err)
	}
	defer rows.Close()
	var out []Save
	for rows.Next() {
		var v Save
		if err = rows.Scan(&v.ID, &v.ListingID, &v.Price, &v.At); err != nil {
			return nil, fmt.Errorf("scan save: %w", err)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	return out, nil
}

func (s pgSaves) SavedAmong(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id FROM saved_listings WHERE account_id = @account AND listing_id = ANY(@ids)`,
		pgx.StrictNamedArgs{"account": account, "ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan save: %w", err)
		}
		out[id] = true
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("save rows: %w", err)
	}
	return out, nil
}

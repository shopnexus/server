package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

type pgListings struct{ db *pgxpool.Pool }

func NewListings(db *pgxpool.Pool) Listings { return pgListings{db} }

const listingColumns = `id, seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
       photos, marks, specs, status, rejection, write_id, created_at, updated_at, published_at, sold_at, deleted_at`

func scanListing(row pgx.Row) (domain.Listing, error) {
	var (
		st                          domain.ListingState
		photos, marks, specs, rejec []byte
	)
	if err := row.Scan(&st.ID, &st.SellerID, &st.Title, &st.Price, &st.Description, &st.CategoryCode, &st.Condition,
		&st.AllowOffers, &st.CommuneCode, &photos, &marks, &specs, &st.Status, &rejec, &st.WriteID, &st.CreatedAt,
		&st.UpdatedAt, &st.PublishedAt, &st.SoldAt, &st.DeletedAt); err != nil {
		return domain.Listing{}, fmt.Errorf("scan listing: %w", err)
	}
	for _, f := range []struct {
		raw []byte
		dst any
	}{{photos, &st.Photos}, {marks, &st.Marks}, {specs, &st.Specs}, {rejec, &st.Rejection}} {
		if f.raw == nil {
			continue
		}
		if err := json.Unmarshal(f.raw, f.dst); err != nil {
			return domain.Listing{}, fmt.Errorf("decode listing json: %w", err)
		}
	}
	return domain.RestoreListing(st), nil
}

// args are a listing's columns as named arguments.
func args(st domain.ListingState) pgx.StrictNamedArgs {
	var rejection any
	if len(st.Rejection) > 0 {
		rejection = st.Rejection
	}
	marks, specs := st.Marks, st.Specs
	if marks == nil {
		marks = []domain.Mark{}
	}
	if specs == nil {
		specs = []domain.Spec{}
	}
	return pgx.StrictNamedArgs{
		"id": st.ID, "seller_id": st.SellerID, "title": st.Title, "price": st.Price, "description": st.Description,
		"category_code": st.CategoryCode, "condition": st.Condition, "allow_offers": st.AllowOffers,
		"commune_code": st.CommuneCode, "photos": st.Photos, "marks": marks, "specs": specs, "status": st.Status,
		"rejection": rejection, "write_id": st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		"published_at": st.PublishedAt, "sold_at": st.SoldAt, "deleted_at": st.DeletedAt,
	}
}

// lockSeller makes the inserts and saves that may add to a seller's
// listings on sale wait for each other, so a count they read stays true
// until they commit.
func lockSeller(ctx context.Context, tx pgx.Tx, seller uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@seller::text))`, pgx.StrictNamedArgs{"seller": seller}); err != nil {
		return fmt.Errorf("lock seller: %w", err)
	}
	return nil
}

// insertListing adds the row unless the seller is at the cap.
const insertListing = `
INSERT INTO listings (` + listingColumns + `)
SELECT @id, @seller_id, @title, @price, @description, @category_code, @condition, @allow_offers, @commune_code,
       @photos, @marks, @specs, @status, @rejection, @write_id, @created_at, @updated_at, @published_at, @sold_at,
       @deleted_at
WHERE (SELECT count(*) FROM listings WHERE seller_id = @seller_id AND status = ANY(@on_sale)) < @limit
ON CONFLICT (id) DO NOTHING`

func (s pgListings) Insert(ctx context.Context, l domain.Listing) error {
	st := l.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockSeller(ctx, tx, st.SellerID); err != nil {
			return err
		}
		a := args(st)
		a["on_sale"], a["limit"] = domain.OnSale, domain.MaxOnSale
		tag, err := tx.Exec(ctx, insertListing, a)
		if err != nil {
			return fmt.Errorf("insert listing: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM listings WHERE id = @id)`, pgx.StrictNamedArgs{"id": st.ID}).Scan(&exists); err != nil {
				return fmt.Errorf("check listing: %w", err)
			}
			if !exists {
				return domain.ErrCapReached
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("insert listing: %w", err)
	}
	return nil
}

func (s pgListings) Discard(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listings WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete listing: %w", err)
	}
	return nil
}

func (s pgListings) CountOnSale(ctx context.Context, seller uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM listings WHERE seller_id = @seller AND status = ANY(@on_sale)`,
		pgx.StrictNamedArgs{"seller": seller, "on_sale": domain.OnSale}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count listings: %w", err)
	}
	return n, nil
}

func (s pgListings) Get(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	l, err := scanListing(s.db.QueryRow(ctx, `SELECT `+listingColumns+` FROM listings WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Listing{}, ErrNotFound
	}
	if err != nil {
		return domain.Listing{}, fmt.Errorf("select listing: %w", err)
	}
	return l, nil
}

const saveListing = `
UPDATE listings
SET title = @title, price = @price, description = @description, category_code = @category_code,
    condition = @condition, allow_offers = @allow_offers, commune_code = @commune_code, photos = @photos,
    marks = @marks, specs = @specs, status = @status, rejection = @rejection, updated_at = @updated_at,
    published_at = @published_at, sold_at = @sold_at, deleted_at = @deleted_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
  AND (status = ANY(@on_sale) OR NOT @status = ANY(@on_sale)
       OR (SELECT count(*) FROM listings o WHERE o.seller_id = listings.seller_id AND o.status = ANY(@on_sale)) < @limit)`

func (s pgListings) Save(ctx context.Context, l *domain.Listing, writeID uuid.UUID, evs ...events.Event) error {
	st := l.State()
	a := args(st)
	a["read_write_id"], a["write_id"], a["on_sale"], a["limit"] = st.WriteID, writeID, domain.OnSale, domain.MaxOnSale
	delete(a, "seller_id")
	delete(a, "created_at")
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if slices.Contains(domain.OnSale, st.Status) {
			if err := lockSeller(ctx, tx, st.SellerID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, saveListing, a)
		if err != nil {
			return fmt.Errorf("update listing: %w", err)
		}
		if tag.RowsAffected() == 0 {
			// The write ID matches, so the cap refused the listing back on
			// sale; otherwise someone wrote it meanwhile.
			var current bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM listings
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id))`,
				pgx.StrictNamedArgs{"id": st.ID, "read_write_id": st.WriteID, "write_id": writeID}).Scan(&current); err != nil {
				return fmt.Errorf("check listing: %w", err)
			}
			if current {
				return domain.ErrCapReached
			}
			return ErrConflict
		}
		for _, e := range evs {
			if err := events.Insert(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save listing: %w", err)
	}
	l.Saved(writeID)
	return nil
}

func (s pgListings) ListBySeller(ctx context.Context, seller uuid.UUID, statuses []domain.Status, p page.Request) ([]domain.Listing, error) {
	rows, err := s.db.Query(ctx, `SELECT `+listingColumns+` FROM listings
WHERE seller_id = @seller AND status = ANY(@statuses) AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"seller": seller, "statuses": statuses, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	return collect(rows)
}

func (s pgListings) GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Listing, error) {
	rows, err := s.db.Query(ctx, `SELECT `+listingColumns+` FROM listings WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	return collect(rows)
}

// collect scans every row of a listings query.
func collect(rows pgx.Rows) ([]domain.Listing, error) {
	defer rows.Close()
	var out []domain.Listing
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	return out, nil
}

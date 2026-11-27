package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgListings struct{ db *pgxpool.Pool }

func NewListings(db *pgxpool.Pool) Listings { return pgListings{db} }

const listingColumns = `id, seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
       photos, specs, status, rejection, reserved_for, write_id, created_at, updated_at, published_at, sold_at,
       deleted_at, lat, lng, held_at, previous_price, price_changed_at, sold_order_id, code`

func scanListing(row pgx.CollectableRow) (domain.Listing, error) {
	var (
		st            domain.ListingState
		reservedFor   *uuid.UUID
		previousPrice *int64
	)
	if err := row.Scan(&st.ID, &st.SellerID, &st.Title, &st.Price, &st.Description, &st.CategoryCode, &st.Condition,
		&st.AllowOffers, &st.CommuneCode, &st.Photos, &st.Specs, &st.Status, &st.Rejection, &reservedFor, &st.WriteID, &st.CreatedAt,
		&st.UpdatedAt, &st.PublishedAt, &st.SoldAt, &st.DeletedAt, &st.Lat, &st.Lng, &st.HeldAt, &previousPrice, &st.PriceChangedAt,
		&st.SoldOrder, &st.Code, &st.BumpedAt); err != nil {
		return domain.Listing{}, fmt.Errorf("scan listing: %w", err)
	}
	if reservedFor != nil {
		st.ReservedFor = *reservedFor
	}
	if previousPrice != nil {
		st.PreviousPrice = *previousPrice
	}
	return domain.RestoreListing(st), nil
}

// args are a listing's columns as named arguments.
func args(st domain.ListingState) pgx.StrictNamedArgs {
	var rejection any
	if len(st.Rejection) > 0 {
		rejection = st.Rejection
	}
	var reservedFor any
	if st.ReservedFor != (uuid.UUID{}) {
		reservedFor = st.ReservedFor
	}
	var previousPrice any
	if st.PriceChangedAt != nil {
		previousPrice = st.PreviousPrice
	}
	specs := st.Specs
	if specs == nil {
		specs = []domain.Spec{}
	}
	return pgx.StrictNamedArgs{
		"id": st.ID, "seller_id": st.SellerID, "title": st.Title, "price": st.Price, "description": st.Description,
		"category_code": st.CategoryCode, "condition": st.Condition, "allow_offers": st.AllowOffers,
		"commune_code": st.CommuneCode, "photos": st.Photos, "specs": specs, "status": st.Status,
		"rejection": rejection, "reserved_for": reservedFor, "write_id": st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		"published_at": st.PublishedAt, "sold_at": st.SoldAt, "deleted_at": st.DeletedAt, "lat": st.Lat, "lng": st.Lng,
		"held_at": st.HeldAt, "previous_price": previousPrice, "price_changed_at": st.PriceChangedAt,
		"sold_order_id": st.SoldOrder,
	}
}

// listingSelect is listingColumns and the columns only their own writes
// set.
const listingSelect = listingColumns + `, bumped_at`

// lockSeller makes the inserts and saves that may add to a seller's
// listings on sale wait for each other, so a count they read stays true
// until they commit.
func lockSeller(b *pgx.Batch, seller uuid.UUID) {
	postgres.QueueLock(b, "listings", seller)
}

// insertListing adds the row unless the seller is at the cap or has the
// row or its request key already, with the first of the ID's codes
// (listing_code, migration 0064) no listing holds.
const insertListing = `
INSERT INTO listings (` + listingColumns + `, request_key)
SELECT @id, @seller_id, @title, @price, @description, @category_code, @condition, @allow_offers, @commune_code,
       @photos, @specs, @status, @rejection, @reserved_for, @write_id, @created_at, @updated_at, @published_at, @sold_at,
       @deleted_at, @lat, @lng, @held_at, @previous_price, @price_changed_at, @sold_order_id,
       (SELECT c FROM generate_series(0, 15) a, listing_code(@id, a) c
        WHERE NOT EXISTS (SELECT 1 FROM listings WHERE code = c) ORDER BY a LIMIT 1),
       @request_key
WHERE (SELECT count(*) FROM listings WHERE seller_id = @seller_id AND status = ANY(@on_sale)) < @limit
  AND NOT EXISTS (SELECT 1 FROM listings WHERE ` + listingOfKey + `)
ON CONFLICT (id) DO NOTHING`

// listingOfKey finds the row of an insert or of its request key.
const listingOfKey = `id = @id OR (seller_id = @seller_id AND request_key = @request_key)`

func (s pgListings) Insert(ctx context.Context, l domain.Listing, key uuid.UUID) (uuid.UUID, error) {
	st := l.State()
	a := args(st)
	a["on_sale"], a["limit"], a["request_key"] = domain.OnSale, domain.MaxOnSale, nil
	if key != uuid.Nil() {
		a["request_key"] = key
	}
	var id uuid.UUID
	b := &pgx.Batch{}
	// A share lock on the seller's row: an insert that commits after Erase
	// waits for it and sees the tombstone.
	postgres.QueueLockLiving(b, st.SellerID, accountdomain.ErrSessionRevoked)
	// The seller's lock holds until the transaction ends, so the cap and
	// the key are read one insert at a time.
	lockSeller(b, st.SellerID)
	b.Queue(insertListing, a).Fn = postgres.ExecAs("insert listing", nil)
	b.Queue(`SELECT id FROM listings WHERE `+listingOfKey, pgx.StrictNamedArgs{
		"id": st.ID, "seller_id": st.SellerID, "request_key": a["request_key"],
	}).QueryRow(func(row pgx.Row) error {
		err := row.Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCapReached
		}
		if err != nil {
			return fmt.Errorf("read listing: %w", err)
		}
		return nil
	})
	// QueueLockLiving's callback aborts, so the batch needs a transaction.
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	}); err != nil {
		return uuid.Nil(), fmt.Errorf("insert listing: %w", err)
	}
	return id, nil
}

func (s pgListings) ByKey(ctx context.Context, seller, key uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT id FROM listings WHERE seller_id = @seller AND request_key = @key`,
		pgx.StrictNamedArgs{"seller": seller, "key": key}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil(), ErrNotFound
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("select listing of key: %w", err)
	}
	return id, nil
}

func (s pgListings) IDsByCode(ctx context.Context, codes []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `SELECT code, id FROM listings WHERE code = ANY(@codes)`, pgx.StrictNamedArgs{"codes": codes})
	if err != nil {
		return nil, fmt.Errorf("select listing codes: %w", err)
	}
	var (
		code string
		id   uuid.UUID
	)
	if _, err = pgx.ForEachRow(rows, []any{&code, &id}, func() error {
		out[code] = id
		return nil
	}); err != nil {
		return nil, fmt.Errorf("select listing codes: %w", err)
	}
	return out, nil
}

func (s pgListings) Discard(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM listings WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete listing: %w", err)
	}
	return nil
}

func (s pgListings) CountByStatus(ctx context.Context, seller uuid.UUID) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT status, count(*) FROM listings WHERE seller_id = @seller GROUP BY status`,
		pgx.StrictNamedArgs{"seller": seller}))
}

// CountSold reads listings_seller: a sold listing stays sold or deleted,
// and deleting keeps sold_at.
func (s pgListings) CountSold(ctx context.Context, seller uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM listings
WHERE seller_id = @seller AND status = ANY(@statuses) AND sold_at IS NOT NULL`, pgx.StrictNamedArgs{
		"seller": seller, "statuses": []domain.Status{domain.StatusSold, domain.StatusDeleted},
	}).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count sold listings: %w", err)
	}
	return n, nil
}

func (s pgListings) CountCreatedSince(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT status, count(*) FROM listings WHERE created_at >= @since GROUP BY status`,
		pgx.StrictNamedArgs{"since": since}))
}

// CountAll's GROUP BY reads only listings_seller, which holds the status.
func (s pgListings) CountAll(ctx context.Context) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT status, count(*) FROM listings GROUP BY status`))
}

func (s pgListings) Get(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	rows, err := s.db.Query(ctx, `SELECT `+listingSelect+` FROM listings WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return domain.Listing{}, fmt.Errorf("select listing: %w", err)
	}
	l, err := pgx.CollectExactlyOneRow(rows, scanListing)
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
    specs = @specs, status = @status, rejection = @rejection, reserved_for = @reserved_for,
    updated_at = @updated_at, lat = @lat, lng = @lng,
    published_at = @published_at, sold_at = @sold_at, deleted_at = @deleted_at, held_at = @held_at, write_id = @write_id,
    previous_price = @previous_price, price_changed_at = @price_changed_at, sold_order_id = @sold_order_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

// atCap is true when a listing going on sale would exceed the cap; one on
// sale already counts itself.
const atCap = `
SELECT count(*) >= @limit FROM listings
WHERE seller_id = @seller AND status = ANY(@on_sale)
  AND NOT EXISTS (SELECT 1 FROM listings WHERE id = @id AND status = ANY(@on_sale))`

func (s pgListings) Save(ctx context.Context, l *domain.Listing, writeID uuid.UUID, entry *audit.Entry, evs ...events.Event) error {
	st := l.State()
	a := args(st)
	a["read_write_id"], a["write_id"] = st.WriteID, writeID
	delete(a, "seller_id")
	delete(a, "created_at")
	// The cap and the write_id checks only abort, so one batch carries it all.
	b := &pgx.Batch{}
	if slices.Contains(domain.OnSale, st.Status) {
		lockSeller(b, st.SellerID)
		b.Queue(atCap, pgx.StrictNamedArgs{
			"id": st.ID, "seller": st.SellerID, "on_sale": domain.OnSale, "limit": domain.MaxOnSale,
		}).QueryRow(func(row pgx.Row) error {
			var full bool
			if err := row.Scan(&full); err != nil {
				return fmt.Errorf("count listings on sale: %w", err)
			}
			if full {
				return domain.ErrCapReached
			}
			return nil
		})
	}
	b.Queue(saveListing, a).Exec(func(tag pgconn.CommandTag) error {
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	})
	if entry != nil {
		audit.Queue(b, *entry)
	}
	events.Queue(b, evs...)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("save listing: %w", err)
	}
	l.Saved(writeID)
	return nil
}

func (s pgListings) ListBySeller(ctx context.Context, seller uuid.UUID, statuses []domain.Status, except []uuid.UUID, p page.Request) ([]domain.Listing, error) {
	if except == nil {
		except = []uuid.UUID{} // id <> ALL(NULL) is never true
	}
	rows, err := s.db.Query(ctx, `SELECT `+listingSelect+` FROM listings
WHERE seller_id = @seller AND status = ANY(@statuses) AND id <> ALL(@except) AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"seller": seller, "statuses": statuses, "except": except, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	ls, err := pgx.CollectRows(rows, scanListing)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	return ls, nil
}

func (s pgListings) ListFresh(ctx context.Context, seller uuid.UUID, statuses []domain.Status, except []uuid.UUID, p page.Request) ([]domain.Listing, error) {
	if except == nil {
		except = []uuid.UUID{} // id <> ALL(NULL) is never true
	}
	var cursor any
	if p.Cursor != (uuid.UUID{}) {
		cursor = p.Cursor
	}
	rows, err := s.db.Query(ctx, `SELECT `+listingSelect+` FROM listings
WHERE seller_id = @seller AND status = ANY(@statuses) AND id <> ALL(@except)
  AND (@cursor::uuid IS NULL OR (fresh_at, id) < (SELECT fresh_at, id FROM listings WHERE id = @cursor))
ORDER BY fresh_at DESC, id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"seller": seller, "statuses": statuses, "except": except, "cursor": cursor, "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	ls, err := pgx.CollectRows(rows, scanListing)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	return ls, nil
}

func (s pgListings) Bump(ctx context.Context, seller, id uuid.UUID, at time.Time) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE listings SET bumped_at = greatest(bumped_at, @at)
WHERE id = @id AND seller_id = @seller AND status = @active`,
		pgx.StrictNamedArgs{"id": id, "seller": seller, "at": at, "active": domain.StatusActive})
	if err != nil {
		return false, fmt.Errorf("bump listing: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgListings) IDsBySeller(ctx context.Context, seller uuid.UUID, statuses []domain.Status) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM listings WHERE seller_id = @seller AND status = ANY(@statuses) ORDER BY id DESC`,
		pgx.StrictNamedArgs{"seller": seller, "statuses": statuses})
	if err != nil {
		return nil, fmt.Errorf("select listing ids: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read listing ids: %w", err)
	}
	return ids, nil
}

func (s pgListings) GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Listing, error) {
	rows, err := s.db.Query(ctx, `SELECT `+listingSelect+` FROM listings WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	ls, err := pgx.CollectRows(rows, scanListing)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	return ls, nil
}

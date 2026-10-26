package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

func listing(t *testing.T, seller uuid.UUID) domain.Listing {
	t.Helper()
	l, err := domain.NewListing(uuid.NewV7(), seller, domain.Draft{
		Title: "Xe đạp", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: domain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{uuid.NewV7()},
		Marks: []domain.MarkDraft{{Text: "Tróc sơn"}}, Specs: []domain.Spec{{Label: "Size", Value: "M"}},
	}, domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestInsertGetSave(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	l := listing(t, newAccount(t, db))
	if err := s.Insert(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, l.ID())
	if err != nil || got.Title() != "Xe đạp" || got.Marks()[0].Text != "Tróc sơn" || got.Status() != domain.StatusProcessing {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if err := s.Insert(ctx, l); err != nil {
		t.Fatalf("a retried insert: %v", err)
	}
	got.Published(map[uuid.UUID]domain.PhotoKeys{got.Photos()[0].UploadID: {Photo: "media/a.jpg", Thumb: "media/a_thumb.jpg"}}, now)
	ev := events.Event{ID: uuid.NewV7(), Kind: events.ListingPublished, SubjectID: l.ID(), OccurredAt: now}
	if err := s.Save(ctx, &got, uuid.NewV7(), ev); err != nil {
		t.Fatal(err)
	}
	if err := got.Hide(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatalf("second save: %v", err)
	}
	stale := l
	if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	var kind string
	if err := db.QueryRow(ctx, `SELECT kind FROM events WHERE subject_id = @id`, pgx.StrictNamedArgs{"id": l.ID()}).Scan(&kind); err != nil || kind != "listing.published" {
		t.Fatalf("%q %v", kind, err)
	}
	back, _ := s.Get(ctx, l.ID())
	if back.Photos()[0].Keys.Photo != "media/a.jpg" || back.Status() != domain.StatusHidden {
		t.Fatalf("%+v", back.State())
	}
}

// Thirty concurrent posts by one seller leave exactly twenty.
func TestCapHoldsUnderRace(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	seller := newAccount(t, db)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ok, full int
	)
	for range 30 {
		wg.Go(func() {
			err := s.Insert(context.Background(), listing(t, seller))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, domain.ErrCapReached):
				full++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok != 20 || full != 10 {
		t.Fatalf("%d inserted, %d refused", ok, full)
	}
}

func TestListBySeller(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	seller := newAccount(t, db)
	ids := make([]uuid.UUID, 0, 3)
	for range 3 {
		l := listing(t, seller)
		if err := s.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, l.ID())
	}
	if err := s.Insert(ctx, listing(t, newAccount(t, db))); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListBySeller(ctx, seller, domain.OnSale, page.Request{Limit: 2})
	if err != nil || len(first) != 3 || first[0].ID() != ids[2] {
		t.Fatalf("first page: %d %v", len(first), err)
	}
	rest, _ := s.ListBySeller(ctx, seller, domain.OnSale, page.Request{Cursor: ids[1], Limit: 2})
	if len(rest) != 1 || rest[0].ID() != ids[0] {
		t.Fatalf("after the cursor: %d", len(rest))
	}
	if sold, _ := s.ListBySeller(ctx, seller, []domain.Status{domain.StatusSold}, page.Request{Limit: 2}); len(sold) != 0 {
		t.Fatalf("sold: %d", len(sold))
	}
}

// A listing coming back on sale counts toward the cap: unmarking a sale is
// refused while the seller has twenty on sale.
func TestSaveBackOnSaleHoldsTheCap(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	seller := newAccount(t, db)
	sold := listing(t, seller)
	if err := s.Insert(ctx, sold); err != nil {
		t.Fatal(err)
	}
	sold.Published(map[uuid.UUID]domain.PhotoKeys{}, now)
	if err := sold.MarkSold(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &sold, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	others := make([]domain.Listing, 0, domain.MaxOnSale)
	for range domain.MaxOnSale {
		l := listing(t, seller)
		if err := s.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		others = append(others, l)
	}
	if err := sold.UnmarkSold(now); err != nil {
		t.Fatal(err)
	}
	unsold := sold
	if err := s.Save(ctx, &unsold, uuid.NewV7()); !errors.Is(err, domain.ErrCapReached) {
		t.Fatalf("at the cap: %v", err)
	}
	if err := others[0].Delete(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &others[0], uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &sold, uuid.NewV7()); err != nil {
		t.Fatalf("below the cap: %v", err)
	}
}

// Discard removes a listing the post flow inserted and could not finish.
func TestDiscard(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	l := listing(t, newAccount(t, db))
	if err := s.Insert(ctx, l); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Discard(ctx, l.ID()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(ctx, l.ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
}

func TestGetMany(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	seller := newAccount(t, db)
	a, b := listing(t, seller), listing(t, seller)
	for _, l := range []domain.Listing{a, b} {
		if err := s.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetMany(ctx, []uuid.UUID{a.ID(), b.ID(), uuid.NewV7()})
	if err != nil || len(got) != 2 {
		t.Fatalf("%d %v", len(got), err)
	}
}

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealrepo "github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
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

// order stores a requested order of buyer's on a new listing of seller's
// and returns the order and the listing.
func order(t *testing.T, db *pgxpool.Pool, buyer, seller uuid.UUID) (orderID, listingID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{uuid.NewV7()},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalogrepo.NewListings(db).Insert(ctx, l); err != nil {
		t.Fatal(err)
	}
	deals := dealrepo.NewDeals(db)
	d := dealdomain.Open(uuid.NewV7(), l.ID(), buyer, seller, now)
	if _, err = deals.Insert(ctx, d); err != nil {
		t.Fatal(err)
	}
	onSale := dealdomain.Listing{Price: 1_500_000, Status: dealdomain.ListingActive}
	r, err := dealdomain.NewRecipient("Hân", "0912345678", "12 Lý Thường Kiệt", "00004", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Accept(dealdomain.Buyer, 1_500_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = d.RequestCod(dealdomain.Buyer, r, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = deals.Save(ctx, &d, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return d.Order().ID, l.ID()
}

func review(t *testing.T, s repository.Reviews, o, seller, buyer, listing uuid.UUID, stars int) domain.Review {
	t.Helper()
	r, err := domain.NewReview(uuid.NewV7(), o, seller, buyer, listing, stars, "Ổn", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReviews(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, one, two := newAccount(t, db), newAccount(t, db), newAccount(t, db)
	o1, l1 := order(t, db, one, seller)
	o2, l2 := order(t, db, two, seller)
	r1 := review(t, s, o1, seller, one, l1, 4)
	r2 := review(t, s, o2, seller, two, l2, 2)
	if avg, n, err := s.Summary(ctx, seller); err != nil || n != 2 || avg != 3 {
		t.Fatalf("summary %v %d %v", avg, n, err)
	}
	if err := r1.Edit(5, "Rất tốt", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &r1, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ByOrder(ctx, o1)
	if err != nil || got.Stars() != 5 || got.Text() != "Rất tốt" || !got.CreatedAt().Equal(now) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if avg, n, _ := s.Summary(ctx, seller); n != 2 || avg != 3.5 {
		t.Fatalf("after the edit %v %d", avg, n)
	}
	list, err := s.OfSeller(ctx, seller, page.Request{})
	if err != nil || len(list) != 2 || list[0].OrderID() != o2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if err = r2.Delete(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &r2, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if avg, n, _ := s.Summary(ctx, seller); n != 1 || avg != 5 {
		t.Fatalf("after the delete %v %d", avg, n)
	}
	if got, err = s.ByOrder(ctx, o2); err != nil || !got.Deleted() || got.Text() != "" {
		t.Fatalf("deleted: %+v %v", got.State(), err)
	}
	if avg, n, err := s.Summary(ctx, newAccount(t, db)); err != nil || n != 0 || avg != 0 {
		t.Fatalf("no reviews: %v %d %v", avg, n, err)
	}
}

func TestReviewWritesConflict(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	o, l := order(t, db, buyer, seller)
	first := review(t, s, o, seller, buyer, l, 5)
	second, err := domain.NewReview(uuid.NewV7(), o, seller, buyer, l, 1, "Lần hai", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, second); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a second first review: %v", err)
	}
	stale, err := s.ByOrder(ctx, o)
	if err != nil || stale.ID() != first.ID() {
		t.Fatalf("%+v %v", stale.State(), err)
	}
	fresh := stale
	if err = fresh.Delete(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &fresh, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err = stale.Edit(2, "Cũ", now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a stale edit: %v", err)
	}
	if _, n, _ := s.Summary(ctx, seller); n != 0 {
		t.Fatalf("a deleted review counted: %d", n)
	}
	if list, _ := s.OfSeller(ctx, seller, page.Request{}); len(list) != 0 {
		t.Fatalf("a deleted review listed: %d", len(list))
	}
}

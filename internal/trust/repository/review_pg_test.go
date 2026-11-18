package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/chattest"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealrepo "github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// order stores a requested order of buyer's on a new listing of seller's
// and returns the order and the listing.
func order(t *testing.T, db *pgxpool.Pool, buyer, seller uuid.UUID) (orderID, listingID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	listing := catalogtest.Listing(t, db, seller)
	deals := dealrepo.NewDeals(db)
	d := dealdomain.Open(uuid.NewV7(), chattest.Pair(t, db, buyer, seller), listing, buyer, seller, now)
	if _, err := deals.Insert(ctx, d); err != nil {
		t.Fatal(err)
	}
	onSale := dealdomain.Listing{Price: 1_500_000, Status: dealdomain.ListingActive}
	r := dealdomain.Recipient{Name: "Hân", Phone: "+84912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: dealdomain.AddressHome}
	if err := d.Accept(dealdomain.Buyer, 1_500_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := d.RequestCod(dealdomain.Buyer, r, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := deals.Save(ctx, &d, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return d.Order().ID, listing
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
	seller, one, two := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	o1, l1 := order(t, db, one, seller)
	o2, l2 := order(t, db, two, seller)
	r1 := review(t, s, o1, seller, one, l1, 4)
	r2 := review(t, s, o2, seller, two, l2, 2)
	var posted int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'review.posted' AND id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": []uuid.UUID{r1.ID(), r2.ID()}}).Scan(&posted); err != nil || posted != 2 {
		t.Fatalf("one review.posted per review: %d %v", posted, err)
	}
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
	// The batch read returns only the buyer's reviews, deleted ones too,
	// and skips an order without one.
	o3, l3 := order(t, db, one, seller)
	r3 := review(t, s, o3, seller, one, l3, 3)
	orders := []uuid.UUID{o1, o2, o3}
	mine, err := s.ByOrders(ctx, one, orders)
	if err != nil || len(mine) != 2 {
		t.Fatalf("one's reviews: %d %v", len(mine), err)
	}
	byOrder := map[uuid.UUID]domain.Review{mine[0].OrderID(): mine[0], mine[1].OrderID(): mine[1]}
	if a, c := byOrder[o1], byOrder[o3]; a.ID() != r1.ID() || a.Stars() != 5 || c.ID() != r3.ID() {
		t.Fatalf("one's reviews: %+v %+v", a.State(), c.State())
	}
	theirs, err := s.ByOrders(ctx, two, orders)
	if err != nil || len(theirs) != 1 || theirs[0].ID() != r2.ID() || !theirs[0].Deleted() {
		t.Fatalf("two's reviews: %d %v", len(theirs), err)
	}
	if none, noneErr := s.ByOrders(ctx, one, nil); noneErr != nil || len(none) != 0 {
		t.Fatalf("no orders: %d %v", len(none), noneErr)
	}
	if avg, n, err := s.Summary(ctx, accounttest.New(t, db)); err != nil || n != 0 || avg != 0 {
		t.Fatalf("no reviews: %v %d %v", avg, n, err)
	}
}

func TestReviewWritesConflict(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
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

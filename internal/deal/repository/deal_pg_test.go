package repository_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

var onSale = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingActive}

func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// newListing inserts a listing of seller's and returns its ID.
func newListing(t *testing.T, db *pgxpool.Pool, seller uuid.UUID) uuid.UUID {
	t.Helper()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		AllowOffers: true, CommuneCode: "00004", Photos: []uuid.UUID{uuid.NewV7()},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalogrepo.NewListings(db).Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l.ID()
}

// inserted opens buyer's deal on listing and stores it.
func inserted(t *testing.T, s repository.Deals, listing, buyer, seller uuid.UUID) domain.Deal {
	t.Helper()
	d := domain.Open(uuid.NewV7(), listing, buyer, seller, now)
	if ok, err := s.Insert(context.Background(), d); err != nil || !ok {
		t.Fatalf("insert: %v %v", ok, err)
	}
	return d
}

func moved(id uuid.UUID) events.Event {
	return events.Event{ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: id, OccurredAt: now}
}

func TestInsertFindGet(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	listing := newListing(t, db, seller)
	d := inserted(t, s, listing, buyer, seller)
	again, err := s.Insert(ctx, domain.Open(uuid.NewV7(), listing, buyer, seller, now))
	if err != nil || again {
		t.Fatalf("second deal of the pair: %v %v", again, err)
	}
	found, err := s.Find(ctx, listing, buyer)
	if err != nil || found.ID() != d.ID() {
		t.Fatalf("find: %v %v", found.ID(), err)
	}
	got, err := s.Get(ctx, d.ID())
	if err != nil || got.Status() != domain.StatusNegotiating || got.Open() != nil || got.SellerID() != seller {
		t.Fatalf("get: %+v %v", got.State(), err)
	}
	if _, err = s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err = s.Find(ctx, listing, newAccount(t, db)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("another buyer: %v", err)
	}
}

func TestSaveWritesMovesAndEvent(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	d := inserted(t, s, newListing(t, db, seller), buyer, seller)
	if err := d.Offer(domain.Buyer, 1_000_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	offer := d.Pending()[0].ID
	if err := s.Save(ctx, &d, uuid.NewV7(), moved(d.ID())); err != nil {
		t.Fatal(err)
	}
	var carried []string
	if err := db.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(payload->'moves')) FROM events
WHERE subject_id = @id AND kind = 'deal.moved'`, pgx.StrictNamedArgs{"id": d.ID()}).Scan(&carried); err != nil ||
		len(carried) != 1 || carried[0] != offer.String() {
		t.Fatalf("deal.moved carries the moves: %v %v", carried, err)
	}
	got, err := s.Get(ctx, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	if o := got.Open(); o == nil || o.By != domain.Buyer || o.Amount != 1_000_000 || got.State().ListedPrice != 1_500_000 {
		t.Fatalf("%+v", got.State())
	}
	moves, err := s.Moves(ctx, []uuid.UUID{d.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if m := moves[d.ID()]; len(m) != 1 || m[0].Kind != domain.MoveOffer || m[0].By != domain.Buyer || m[0].Amount != 1_000_000 {
		t.Fatalf("%+v", moves)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE subject_id = @id AND kind = 'deal.moved'`,
		pgx.StrictNamedArgs{"id": d.ID()}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events: %d %v", n, err)
	}

	if err = got.Accept(domain.Buyer, 1_500_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	agreed, err := s.Get(ctx, d.ID())
	if err != nil || agreed.Status() != domain.StatusAgreed || agreed.AgreedPrice() != 1_500_000 ||
		agreed.AgreedAt() == nil || agreed.Open() != nil {
		t.Fatalf("%+v %v", agreed.State(), err)
	}
	if err = agreed.Cancel(domain.Seller, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &agreed, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.Get(ctx, d.ID())
	if err != nil || cancelled.Status() != domain.StatusNegotiating || cancelled.AgreedAt() != nil {
		t.Fatalf("%+v %v", cancelled.State(), err)
	}
	moves, err = s.Moves(ctx, []uuid.UUID{d.ID()})
	if m := moves[d.ID()]; err != nil || len(m) != 3 || m[2].Kind != domain.MoveCancel || m[2].Amount != 0 {
		t.Fatalf("%+v %v", moves, err)
	}
}

func TestSaveRefusesAStaleDeal(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	d := inserted(t, s, newListing(t, db, seller), buyer, seller)
	first, err := s.Get(ctx, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Get(ctx, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Offer(domain.Buyer, 1_000_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &first, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err = second.Offer(domain.Buyer, 1_100_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &second, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	moves, err := s.Moves(ctx, []uuid.UUID{d.ID()})
	if err != nil || len(moves[d.ID()]) != 1 {
		t.Fatalf("%+v %v", moves, err)
	}
}

func TestListByPartyAndOfListing(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer1, buyer2 := newAccount(t, db), newAccount(t, db), newAccount(t, db)
	listing1, listing2 := newListing(t, db, seller), newListing(t, db, seller)
	a := inserted(t, s, listing1, buyer1, seller)
	b := inserted(t, s, listing1, buyer2, seller)
	c := inserted(t, s, listing2, buyer1, seller)

	ids := func(ds []domain.Deal) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(ds))
		for i := range ds {
			out = append(out, ds[i].ID())
		}
		return out
	}
	got, err := s.ListByParty(ctx, seller, domain.Seller, page.Request{Limit: 1})
	if err != nil || len(got) != 2 || got[0].ID() != c.ID() || got[1].ID() != b.ID() {
		t.Fatalf("seller: %v %v", ids(got), err)
	}
	got, err = s.ListByParty(ctx, buyer1, domain.Buyer, page.Request{})
	if err != nil || len(got) != 2 || got[0].ID() != c.ID() || got[1].ID() != a.ID() {
		t.Fatalf("buyer: %v %v", ids(got), err)
	}
	got, err = s.ListByParty(ctx, buyer1, domain.Seller, page.Request{})
	if err != nil || len(got) != 0 {
		t.Fatalf("buyer as seller: %v %v", ids(got), err)
	}
	got, err = s.OfListing(ctx, listing1)
	if err != nil || len(got) != 2 || got[0].ID() != a.ID() || got[1].ID() != b.ID() {
		t.Fatalf("of listing: %v %v", ids(got), err)
	}
}

// requestedDeal stores a deal on a new listing of seller's, agreed and with
// a requested order.
func requestedDeal(t *testing.T, db *pgxpool.Pool, s repository.Deals, buyer, seller uuid.UUID) domain.Deal {
	t.Helper()
	d := inserted(t, s, newListing(t, db, seller), buyer, seller)
	if err := d.Accept(domain.Buyer, 1_500_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := d.RequestCod(domain.Buyer, recipient, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), &d, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return d
}

var recipient = domain.Recipient{Name: "Hân", Phone: "+84912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004"}

func TestOrderRoundTrip(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	d := requestedDeal(t, db, s, buyer, seller)
	got, err := s.Get(ctx, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	o := got.Order()
	if o == nil || o.Status != domain.OrderRequested || !strings.HasPrefix(o.Code, "SN-") || o.Recipient.Name != "Hân" ||
		o.Price != 1_500_000 || o.RequestedAt.IsZero() {
		t.Fatalf("%+v", o)
	}
	w, err := domain.NewWaybill(domain.CarrierOther, "Ahamove", "AH1", 30_000)
	if err != nil {
		t.Fatal(err)
	}
	if err = got.Ship(domain.Seller, w, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Get(ctx, d.ID()); err != nil || got.Order().Waybill == nil || *got.Order().Waybill != w ||
		got.Order().ShippedAt == nil || got.Order().Code != o.Code {
		t.Fatalf("%+v %v", got.Order(), err)
	}
	for _, c := range []struct {
		account uuid.UUID
		as      domain.Party
	}{{buyer, domain.Buyer}, {seller, domain.Seller}} {
		rows, listErr := s.ListOrders(ctx, c.account, c.as, page.Request{})
		if listErr != nil || len(rows) != 1 || rows[0].DealID != d.ID() || rows[0].ListingID != d.ListingID() ||
			rows[0].Order.Status != domain.OrderShipped {
			t.Fatalf("%s: %+v %v", c.as, rows, listErr)
		}
	}
	if err = got.Deliver(domain.Seller, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Get(ctx, d.ID()); err != nil || got.Status() != domain.StatusSold || got.AgreedPrice() != 1_500_000 {
		t.Fatalf("%+v %v", got.State(), err)
	}
}

func TestSaveTwiceWritesOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	d := inserted(t, s, newListing(t, db, seller), buyer, seller)
	before := d.State()
	writeID := uuid.NewV7()
	for range 2 {
		again := domain.RestoreDeal(before)
		if err := again.Offer(domain.Buyer, 1_000_000, onSale, now); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &again, writeID, moved(d.ID())); err != nil {
			t.Fatal(err)
		}
	}
	moves, err := s.Moves(ctx, []uuid.UUID{d.ID()})
	if err != nil || len(moves[d.ID()]) != 1 {
		t.Fatalf("%+v %v", moves, err)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE subject_id = @id`, pgx.StrictNamedArgs{"id": d.ID()}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events: %d %v", n, err)
	}
}

func TestCodLimitHoldsUnderRace(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	for range domain.MaxOpenOrders - 1 {
		requestedDeal(t, db, s, buyer, seller)
	}
	race := func() []error {
		ds := make([]domain.Deal, 2)
		for i := range ds {
			ds[i] = inserted(t, s, newListing(t, db, seller), buyer, seller)
			if err := ds[i].Accept(domain.Buyer, 1_500_000, onSale, now); err != nil {
				t.Fatal(err)
			}
			if err := ds[i].RequestCod(domain.Buyer, recipient, onSale, now); err != nil {
				t.Fatal(err)
			}
		}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range ds {
			wg.Go(func() { errs[i] = s.Save(ctx, &ds[i], uuid.NewV7()) })
		}
		wg.Wait()
		return errs
	}
	errs := race()
	if ok := (errs[0] == nil) != (errs[1] == nil); !ok {
		t.Fatalf("at 2 open orders, two requests: %v", errs)
	}
	for _, err := range race() {
		if !errors.Is(err, domain.ErrCodLimit) {
			t.Fatalf("at 3 open orders: %v", err)
		}
	}
}

func TestOneShippedOrderPerListing(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller := newAccount(t, db)
	listing := newListing(t, db, seller)
	w, err := domain.NewWaybill(domain.CarrierGHN, "", "GHN1", 20_000)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]error, 0, 2)
	for range 2 {
		d := inserted(t, s, listing, newAccount(t, db), seller)
		if err = d.Accept(domain.Buyer, 1_500_000, onSale, now); err != nil {
			t.Fatal(err)
		}
		if err = d.RequestCod(domain.Buyer, recipient, onSale, now); err != nil {
			t.Fatal(err)
		}
		if err = s.Save(ctx, &d, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		if err = d.Ship(domain.Seller, w, onSale, now); err != nil {
			t.Fatal(err)
		}
		results = append(results, s.Save(ctx, &d, uuid.NewV7()))
	}
	if results[0] != nil || results[1] == nil {
		t.Fatalf("%v", results)
	}
}

func TestOrderByID(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller, buyer := newAccount(t, db), newAccount(t, db)
	d := requestedDeal(t, db, s, buyer, seller)
	row, err := s.Order(ctx, d.Order().ID)
	if err != nil || row.DealID != d.ID() || row.BuyerID != buyer || row.SellerID != seller || row.Order.Status != domain.OrderRequested {
		t.Fatalf("%+v %v", row, err)
	}
	if _, err = s.Order(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

package service_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/page"
)

var home = accountdomain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: accountdomain.AddressHome}

// address adds an address to owner's book and returns its ID.
func (e env) address(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	a, err := e.accounts.CreateAddress(context.Background(), owner, uuid.NewV7(), home, false)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// agreed opens buyer's deal on listing and accepts the listed price.
func (e env) agreed(t *testing.T, buyer, listing uuid.UUID) service.DealView {
	t.Helper()
	v := e.open(t, buyer, listing)
	must(t)(e.deals.Accept(context.Background(), buyer, v.ID, 1_500_000))
	return v
}

// pending agrees buyer's deal on listing and asks for COD.
func (e env) pending(t *testing.T, buyer, listing uuid.UUID) service.DealView {
	t.Helper()
	v := e.agreed(t, buyer, listing)
	r, err := e.deals.RequestCod(context.Background(), buyer, v.ID, e.address(t, buyer), "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// UC-DEAL-03.
func TestRequestCod(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	o := v.Order
	if o == nil || o.Status != domain.OrderPending || o.Code == "" || o.Price != 1_500_000 ||
		o.Recipient.Commune == "" || o.Recipient.Province == "" || o.Recipient.Phone != "+84912345678" ||
		o.Recipient.Kind != domain.AddressHome {
		t.Fatalf("%+v", o)
	}
	s := e.get(t, seller, v.ID)
	if s.Order == nil || s.Order.Recipient.Phone == "" || s.Order.Recipient.Street == "" {
		t.Fatalf("seller reads: %+v", s.Order)
	}
	if m := moveKinds(s); m[len(m)-1] != domain.MoveCodRequested {
		t.Fatalf("%v", m)
	}
}

// A repeated Idempotency-Key of the caller runs nothing more: the request
// and the cancel answer the deal again, where a call without a key, or
// with a new one, is refused. A refusal repeats as it was.
//
// UC-DEAL-03 · UC-DEAL-06 · NFR-IDEM-01.
func TestCodAndCancelWithAKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	address, request, cancel := e.address(t, buyer), uuid.NewV7().String(), uuid.NewV7().String()
	first, err := e.deals.RequestCod(ctx, buyer, v.ID, address, request)
	if err != nil || first.Order == nil || first.Order.Status != domain.OrderPending {
		t.Fatalf("first request: %+v %v", first.Order, err)
	}
	again, err := e.deals.RequestCod(ctx, buyer, v.ID, address, request)
	if err != nil || again.Order == nil || again.Order.ID != first.Order.ID {
		t.Fatalf("repeated request: %+v %v", again.Order, err)
	}
	for _, key := range []string{"", uuid.NewV7().String()} {
		if _, err = e.deals.RequestCod(ctx, buyer, v.ID, address, key); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("a second request with key %q: %v", key, err)
		}
	}
	if _, err = e.deals.CancelOrder(ctx, seller, first.Order.ID, cancel); err != nil {
		t.Fatal(err)
	}
	if done, cancelErr := e.deals.CancelOrder(ctx, seller, first.Order.ID, cancel); cancelErr != nil || done.Order.Status != domain.OrderCancelled {
		t.Fatalf("repeated cancel: %+v %v", done.Order, cancelErr)
	}
	if _, err = e.deals.CancelOrder(ctx, seller, first.Order.ID, ""); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("a cancel without a key: %v", err)
	}
	// The buyer's same key is a run of its own, refused on the cancelled
	// order, and that refusal repeats.
	for range 2 {
		if _, err = e.deals.CancelOrder(ctx, buyer, first.Order.ID, cancel); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("the buyer's cancel with the seller's key: %v", err)
		}
	}
}

// The order holds a copy of the address: editing or deleting the address
// afterwards leaves the order's recipient as it was.
//
// UC-DEAL-03.
func TestOrderKeepsItsRecipient(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	address := e.address(t, buyer)
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.RequestCod(ctx, buyer, v.ID, address, ""))
	if _, err := e.accounts.UpdateAddress(ctx, buyer, address, accountdomain.AddressDraft{
		Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004", Kind: accountdomain.AddressHome,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := e.accounts.DeleteAddress(ctx, buyer, address); err != nil {
		t.Fatal(err)
	}
	r := e.get(t, buyer, v.ID).Order.Recipient
	if r.Name != "Hân" || r.Phone != "+84912345678" || r.Street != "12 Lý Thường Kiệt" || r.CommuneCode != "00004" || r.Commune == "" {
		t.Fatalf("%+v", r)
	}
}

// UC-DEAL-03 E1, E2, E3, E4, E5.
func TestRequestCodRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)

	suspended := e.person(t)
	sv := e.agreed(t, suspended, catalogtest.Listing(t, e.db, seller))
	accounttest.Suspend(t, e.accounts, suspended)
	if _, err := e.deals.RequestCod(ctx, suspended, sv.ID, e.address(t, suspended), ""); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended: %v", err)
	}

	buyer := e.person(t)
	mine := e.address(t, buyer)
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, v.ID, e.address(t, seller), ""); !errors.Is(err, accountdomain.ErrAddressNotFound) {
		t.Errorf("someone else's address: %v", err)
	}
	if _, err := e.deals.RequestCod(ctx, buyer, v.ID, uuid.NewV7(), ""); !errors.Is(err, accountdomain.ErrAddressNotFound) {
		t.Errorf("an unknown address: %v", err)
	}
	open := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, open.ID, mine, ""); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("not agreed: %v", err)
	}
	hidden := catalogtest.Listing(t, e.db, seller)
	hv := e.agreed(t, buyer, hidden)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.RequestCod(ctx, buyer, hv.ID, mine, ""); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Errorf("hidden listing: %v", err)
	}
	for range domain.MaxOpenOrders {
		e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	}
	fourth := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, fourth.ID, mine, ""); !errors.Is(err, domain.ErrCodLimit) {
		t.Errorf("fourth order: %v", err)
	}
}

// A COD request waits for the seller to come back from away mode; an order
// placed before goes on.
func TestRequestCodRefusedWhileTheSellerIsAway(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	address := e.address(t, buyer)
	placed := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	accounttest.Away(t, e.accounts, seller)
	if _, err := e.deals.RequestCod(ctx, buyer, v.ID, address, ""); !errors.Is(err, domain.ErrSellerAway) {
		t.Fatalf("cod: %v", err)
	}
	if c, err := e.deals.CancelOrder(ctx, buyer, placed.Order.ID, ""); err != nil || c.Order.Status != domain.OrderCancelled {
		t.Fatalf("cancel the order placed before: %+v %v", c, err)
	}
	if _, err := e.accounts.Update(ctx, seller, accountsvc.UpdateInput{ClearAway: true}); err != nil {
		t.Fatal(err)
	}
	if r, err := e.deals.RequestCod(ctx, buyer, v.ID, address, ""); err != nil || r.Order == nil || r.Order.Status != domain.OrderPending {
		t.Fatalf("cod once back: %+v %v", r, err)
	}
}

// UC-DEAL-06 · UC-DEAL-05 E1.
func TestCancelAPendingOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	c, err := e.deals.Cancel(context.Background(), buyer, v.ID)
	if err != nil || c.Status != domain.StatusNegotiating || c.Order.Status != domain.OrderCancelled ||
		c.Order.CancelReason != domain.CancelByBuyer {
		t.Fatalf("%+v %v", c, err)
	}
	if s := e.get(t, seller, v.ID); s.Order.Recipient.Phone != "" || s.Order.Recipient.Street != "" || s.Order.Recipient.Name != "Hân" {
		t.Fatalf("seller reads a cancelled order: %+v", s.Order.Recipient)
	}
}

func TestReservationAloneCancelsNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, one, two := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first, second := e.pending(t, one, listing), e.pending(t, two, listing)
	// A ship flow reserved the listing and has not saved the order yet.
	if err := e.catalog.Reserve(ctx, listing, first.Order.ID); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if v := e.get(t, two, second.ID); v.Order.Status != domain.OrderPending {
		t.Fatalf("the other buyer's request: %+v", v.Order)
	}
}

// UC-DEAL-06 A3 · BR-DEAL-04.
func TestShipCancelsOtherPendingOrders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, one, two := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first, second := e.pending(t, one, listing), e.pending(t, two, listing)
	must(t)(e.deals.CreateShipment(ctx, seller, first.Order.ID, key(), manual("SPXVN0123")))
	if v := e.get(t, two, second.ID); v.Order.Status != domain.OrderCancelled || v.Order.CancelReason != domain.CancelShippedToAnother ||
		v.Status != domain.StatusNegotiating {
		t.Fatalf("other buyer: %+v %s", v.Order, v.Status)
	}
	if v := e.get(t, one, first.ID); v.Order.Status != domain.OrderShipped {
		t.Fatalf("first buyer: %+v", v.Order)
	}
}

func TestListOrders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	for _, c := range []struct {
		viewer uuid.UUID
		as     domain.Party
	}{{buyer, domain.Buyer}, {seller, domain.Seller}} {
		p, err := e.deals.ListOrders(ctx, c.viewer, c.as, page.Request{})
		if err != nil || len(p.Items) != 1 || p.Items[0].DealID != v.ID || p.Items[0].Listing.Title == "" ||
			p.Items[0].Code != v.Order.Code {
			t.Fatalf("%s: %+v %v", c.as, p, err)
		}
	}
	if p, _ := e.deals.ListOrders(ctx, buyer, domain.Buyer, page.Request{}); p.Items[0].CounterpartID != seller {
		t.Fatalf("buyer's counterpart: %s", p.Items[0].CounterpartID)
	}
	if p, _ := e.deals.ListOrders(ctx, seller, domain.Seller, page.Request{}); p.Items[0].CounterpartID != buyer {
		t.Fatalf("seller's counterpart: %s", p.Items[0].CounterpartID)
	}
}

// readListings records the listings each Summaries call asks for.
type readListings struct {
	catalogsvc.Service

	mu    sync.Mutex
	asked [][]uuid.UUID
}

func (r *readListings) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]catalogsvc.Summary, error) {
	r.mu.Lock()
	r.asked = append(r.asked, slices.Clone(ids))
	r.mu.Unlock()
	return r.Service.Summaries(ctx, ids)
}

// The look-ahead row of a page sets the cursor, and its listing is not
// read.
func TestListOrdersPages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	older := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	newer := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	rec := &readListings{Service: e.catalog}
	deps := e.deps
	deps.Catalog = rec
	deals := service.New(deps)
	first, err := deals.ListOrders(ctx, buyer, domain.Buyer, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != newer.Order.ID || first.NextCursor == nil || *first.NextCursor != newer.Order.ID {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if len(rec.asked) != 1 || !slices.Equal(rec.asked[0], []uuid.UUID{newer.Listing.ID}) {
		t.Fatalf("listings read for the first page: %v", rec.asked)
	}
	second, err := deals.ListOrders(ctx, buyer, domain.Buyer, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != older.Order.ID || second.NextCursor != nil {
		t.Fatalf("second page: %+v %v", second, err)
	}
}

// UC-DEAL-06 A3.
func TestHidingCancelsRequests(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := e.catalog.Hide(context.Background(), seller, listing); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderCancelled || got.Order.CancelReason != domain.CancelListingClosed {
		t.Fatalf("%+v", got.Order)
	}
}

func TestOrderFacts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	f, ok, err := e.deals.Order(context.Background(), v.Order.ID)
	if err != nil || !ok || f.BuyerID != buyer || f.SellerID != seller || f.ListingID != listing ||
		f.Status != domain.OrderPending {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.deals.Order(context.Background(), uuid.NewV7()); err != nil || ok {
		t.Fatalf("unknown: %v %v", ok, err)
	}
}

func TestOrderFactsCarryTheShipmentAndWhoCompleted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPX0042")
	f, ok, err := e.deals.Order(ctx, v.Order.ID)
	if err != nil || !ok || f.Shipment == nil || f.Shipment.TrackingCode != "SPX0042" || f.Code == "" || f.Price == 0 ||
		f.ShippedAt == nil || f.CompletedBy != "" {
		t.Fatalf("shipped: %+v %v %v", f, ok, err)
	}
	if _, err = e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key()); err != nil {
		t.Fatal(err)
	}
	if f, _, err = e.deals.Order(ctx, v.Order.ID); err != nil || f.CompletedBy != domain.Buyer || f.ClosedAt == nil {
		t.Fatalf("completed: %+v %v", f, err)
	}
}

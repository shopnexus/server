package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

var home = accountdomain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004"}

// address adds an address to owner's book and returns its ID.
func (e env) address(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	a, err := e.accounts.CreateAddress(context.Background(), owner, uuid.NewV7(), home)
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

// requested agrees buyer's deal on listing and asks for COD.
func (e env) requested(t *testing.T, buyer, listing uuid.UUID) service.DealView {
	t.Helper()
	v := e.agreed(t, buyer, listing)
	r, err := e.deals.RequestCod(context.Background(), buyer, v.ID, e.address(t, buyer))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRequestCod(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.requested(t, buyer, e.listing(t, seller))
	o := v.Order
	if o == nil || o.Status != domain.OrderRequested || o.Code == "" || o.Price != 1_500_000 ||
		o.Recipient.Commune == "" || o.Recipient.Province == "" || o.Recipient.Phone != "+84912345678" {
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

// The order holds a copy of the address: editing or deleting the address
// afterwards leaves the order's recipient as it was.
func TestOrderKeepsItsRecipient(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	address := e.address(t, buyer)
	v := e.agreed(t, buyer, e.listing(t, seller))
	must(t)(e.deals.RequestCod(ctx, buyer, v.ID, address))
	if _, err := e.accounts.UpdateAddress(ctx, buyer, address, accountdomain.AddressDraft{
		Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004",
	}); err != nil {
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

func TestRequestCodRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)

	suspended := e.person(t)
	sv := e.agreed(t, suspended, e.listing(t, seller))
	e.suspend(t, suspended)
	if _, err := e.deals.RequestCod(ctx, suspended, sv.ID, e.address(t, suspended)); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended: %v", err)
	}

	buyer := e.person(t)
	mine := e.address(t, buyer)
	v := e.agreed(t, buyer, e.listing(t, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, v.ID, e.address(t, seller)); !errors.Is(err, accountdomain.ErrAddressNotFound) {
		t.Errorf("someone else's address: %v", err)
	}
	if _, err := e.deals.RequestCod(ctx, buyer, v.ID, uuid.NewV7()); !errors.Is(err, accountdomain.ErrAddressNotFound) {
		t.Errorf("an unknown address: %v", err)
	}
	open := e.open(t, buyer, e.listing(t, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, open.ID, mine); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("not agreed: %v", err)
	}
	hidden := e.listing(t, seller)
	hv := e.agreed(t, buyer, hidden)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.RequestCod(ctx, buyer, hv.ID, mine); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Errorf("hidden listing: %v", err)
	}
	for range domain.MaxOpenOrders {
		e.requested(t, buyer, e.listing(t, seller))
	}
	fourth := e.agreed(t, buyer, e.listing(t, seller))
	if _, err := e.deals.RequestCod(ctx, buyer, fourth.ID, mine); !errors.Is(err, domain.ErrCodLimit) {
		t.Errorf("fourth order: %v", err)
	}
}

func TestCancelARequestedOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.requested(t, buyer, e.listing(t, seller))
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
	listing := e.listing(t, seller)
	first, second := e.requested(t, one, listing), e.requested(t, two, listing)
	// A ship flow reserved the listing and has not saved the order yet.
	if err := e.catalog.Reserve(ctx, listing, first.Order.ID); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if v := e.get(t, two, second.ID); v.Order.Status != domain.OrderRequested {
		t.Fatalf("the other buyer's request: %+v", v.Order)
	}
}

func TestShipCancelsOtherRequests(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, one, two := e.person(t), e.person(t), e.person(t)
	listing := e.listing(t, seller)
	first, second := e.requested(t, one, listing), e.requested(t, two, listing)
	must(t)(e.deals.Ship(ctx, seller, first.ID, key(), spx))
	if v := e.get(t, two, second.ID); v.Order.Status != domain.OrderCancelled || v.Order.CancelReason != domain.CancelSentToAnother ||
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
	v := e.requested(t, buyer, e.listing(t, seller))
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
}

func TestHidingCancelsRequests(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)
	v := e.requested(t, buyer, listing)
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
	listing := e.listing(t, seller)
	v := e.requested(t, buyer, listing)
	f, err := e.deals.Order(context.Background(), v.Order.ID)
	if err != nil || f.DealID != v.ID || f.BuyerID != buyer || f.SellerID != seller || f.ListingID != listing ||
		f.Status != domain.OrderRequested {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err = e.deals.Order(context.Background(), uuid.NewV7()); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

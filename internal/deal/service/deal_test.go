package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// UC-DEAL-02.
func TestNegotiateToAgreement(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)

	v, created, err := e.deals.Open(ctx, buyer, listing)
	if err != nil || !created {
		t.Fatalf("open: %v %v", created, err)
	}
	if v.Role != domain.Buyer || v.Status != domain.StatusNegotiating || v.Listing.CoverThumbURL == "" ||
		v.OfferRange == nil || *v.OfferRange != (domain.Range{Min: 750_000, Max: 1_499_000}) {
		t.Fatalf("%+v", v)
	}
	again, created, err := e.deals.Open(ctx, buyer, listing)
	if err != nil || created || again.ID != v.ID {
		t.Fatalf("open again: %v %v %v", again.ID, created, err)
	}

	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	s := e.get(t, seller, v.ID)
	if s.Role != domain.Seller || s.OpenPrice == nil || s.OpenPrice.By != domain.Buyer || s.OpenPrice.Amount != 1_000_000 ||
		s.OfferRange == nil || *s.OfferRange != (domain.Range{Min: 1_001_000, Max: 1_499_000}) {
		t.Fatalf("%+v", s)
	}
	must(t)(e.deals.Offer(ctx, seller, v.ID, 1_300_000))
	agreed, err := e.deals.Accept(ctx, buyer, v.ID, 1_300_000)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.MoveKind{domain.MoveOffer, domain.MoveCounter, domain.MoveAccept}
	if agreed.Status != domain.StatusAgreed || agreed.AgreedPrice == nil || *agreed.AgreedPrice != 1_300_000 ||
		agreed.OfferRange != nil || !slices.Equal(moveKinds(agreed), want) {
		t.Fatalf("%+v", agreed)
	}
	if k := pgtest.EventKinds(t, e.db, v.ID); len(k) != 3 || k[0] != "deal.moved" {
		t.Fatalf("events %v", k)
	}

	for _, c := range []struct {
		viewer uuid.UUID
		as     domain.Party
		n      int
	}{{buyer, domain.Buyer, 1}, {seller, domain.Seller, 1}, {buyer, domain.Seller, 0}} {
		p, listErr := e.deals.ListMine(ctx, c.viewer, c.as, page.Request{})
		if listErr != nil || len(p.Items) != c.n {
			t.Fatalf("%s: %+v %v", c.as, p, listErr)
		}
		if c.n == 1 && (p.Items[0].ID != v.ID || len(p.Items[0].Moves) != 3) {
			t.Fatalf("%s: %+v", c.as, p.Items[0])
		}
	}

	cancelled, err := e.deals.Cancel(ctx, seller, v.ID)
	if err != nil || cancelled.Status != domain.StatusNegotiating || cancelled.AgreedPrice != nil {
		t.Fatalf("%+v %v", cancelled, err)
	}
	if m := cancelled.Moves[len(cancelled.Moves)-1]; m.Kind != domain.MoveCancel || m.By != domain.Seller {
		t.Fatalf("%+v", m)
	}
}

// UC-DEAL-02 · BR-DEAL-04.
func TestTwoBuyersDealApart(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, one, two := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first, second := e.open(t, one, listing), e.open(t, two, listing)
	must(t)(e.deals.Offer(ctx, one, first.ID, 1_200_000))
	must(t)(e.deals.Accept(ctx, seller, first.ID, 1_200_000))
	if v := e.get(t, two, second.ID); v.Status != domain.StatusNegotiating {
		t.Fatalf("%+v", v)
	}
	must(t)(e.deals.Offer(ctx, two, second.ID, 1_000_000))
}

// UC-DEAL-02 A1, E1, E2, E3, E5.
func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	hidden := catalogtest.Listing(t, e.db, seller)
	early := e.person(t)
	opened := e.open(t, early, hidden)
	if _, err := e.catalog.Hide(ctx, seller, hidden, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	suspended := e.person(t)
	accounttest.Suspend(t, e.accounts, suspended)

	for _, c := range []struct {
		name    string
		buyer   uuid.UUID
		listing uuid.UUID
		want    error
	}{
		{"own listing", seller, listing, domain.ErrOwnListing},
		{"suspended", suspended, listing, accountdomain.ErrSuspended},
		{"hidden listing", e.person(t), hidden, domain.ErrListingUnavailable},
		{"unknown listing", e.person(t), uuid.NewV7(), catalogdomain.ErrNotFound},
	} {
		if _, _, err := e.deals.Open(ctx, c.buyer, c.listing); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, _, err := e.deals.Open(ctx, accounttest.New(t, e.db), listing); !errors.Is(err, accountdomain.ErrPhoneRequired) {
		t.Fatalf("an account without a phone opens a deal: %v", err)
	}
	v, created, err := e.deals.Open(ctx, early, hidden)
	if err != nil || created || v.ID != opened.ID {
		t.Fatalf("an existing deal on a hidden listing: %v %v", created, err)
	}
}

// BR-ACC-02.
func TestStrangerSeesNoDeal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	v := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	for _, id := range []uuid.UUID{v.ID, uuid.NewV7()} {
		calls := map[string]func() error{
			"get": func() error { _, err := e.deals.Get(ctx, stranger, id); return err },
			"offer": func() error {
				_, err := e.deals.Offer(ctx, stranger, id, 1_100_000)
				return err
			},
			"accept": func() error {
				_, err := e.deals.Accept(ctx, stranger, id, 1_000_000)
				return err
			},
			"decline":  func() error { _, err := e.deals.Decline(ctx, stranger, id); return err },
			"withdraw": func() error { _, err := e.deals.Withdraw(ctx, stranger, id); return err },
			"cancel":   func() error { _, err := e.deals.Cancel(ctx, stranger, id); return err },
		}
		for name, call := range calls {
			if err := call(); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%s %s: %v", name, id, err)
			}
		}
	}
}

// A new listed price voids the seller's counter at once: the buyer's
// accept of it, and an order at it or at the old listed price, are
// price_changed; an order at the new listed price agrees the deal.
//
// UC-DEAL-02 E7.
func TestAcceptAfterThePriceChanged(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.open(t, buyer, listing)
	address := e.address(t, buyer)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	must(t)(e.deals.Offer(ctx, seller, v.ID, 1_300_000))
	e.edit(t, listing, func(d *catalogdomain.ListingInput) { d.Price = 1_400_000 })
	if _, err := e.deals.Accept(ctx, buyer, v.ID, 1_300_000); !errors.Is(err, domain.ErrPriceChanged) {
		t.Fatalf("accept the old counter: %v", err)
	}
	for _, old := range []int64{1_300_000, 1_500_000} {
		if _, err := e.deals.RequestCod(ctx, buyer, v.ID, old, address, ""); !errors.Is(err, domain.ErrPriceChanged) {
			t.Fatalf("order at %d: %v", old, err)
		}
	}
	agreed, err := e.deals.RequestCod(ctx, buyer, v.ID, 1_400_000, address, "")
	if err != nil || agreed.Status != domain.StatusAgreed || *agreed.AgreedPrice != 1_400_000 || agreed.Order.Price != 1_400_000 {
		t.Fatalf("%+v %v", agreed, err)
	}
}

// UC-DEAL-02 A4.
func TestTakeBackOnAHiddenListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, other := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v, agreed := e.open(t, buyer, listing), e.open(t, other, listing)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	must(t)(e.deals.Offer(ctx, other, agreed.ID, 1_100_000))
	must(t)(e.deals.Accept(ctx, seller, agreed.ID, 1_100_000))
	if _, err := e.catalog.Hide(ctx, seller, listing, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.Offer(ctx, seller, v.ID, 1_200_000); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("counter: %v", err)
	}
	if _, err := e.deals.Accept(ctx, seller, v.ID, 1_000_000); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("accept: %v", err)
	}
	if w, err := e.deals.Withdraw(ctx, buyer, v.ID); err != nil || w.OpenPrice != nil {
		t.Fatalf("withdraw: %+v %v", w, err)
	}
	if c, err := e.deals.Cancel(ctx, other, agreed.ID); err != nil || c.Status != domain.StatusNegotiating {
		t.Fatalf("cancel: %+v %v", c, err)
	}
}

// UC-DEAL-02 E1 · BR-MOD-02.
func TestSuspendedCallerTakesBackOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	accounttest.Suspend(t, e.accounts, buyer)
	if _, err := e.deals.Offer(ctx, buyer, v.ID, 1_100_000); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("offer: %v", err)
	}
	if _, err := e.deals.Accept(ctx, buyer, v.ID, 1_500_000); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("accept: %v", err)
	}
	if _, err := e.deals.Withdraw(ctx, buyer, v.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
}

// While the seller is away, the buyer's offers and accepts are refused;
// the seller's moves and a decline go on, and clearing away mode lifts it.
func TestSellerAwayRefusesTheBuyersBindingMoves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, other := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v, ov := e.open(t, buyer, listing), e.open(t, other, listing)
	address := e.address(t, buyer)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	must(t)(e.deals.Offer(ctx, other, ov.ID, 1_000_000))
	accounttest.Away(t, e.accounts, seller)

	must(t)(e.deals.Offer(ctx, seller, v.ID, 1_300_000))
	for name, move := range map[string]func() (service.DealView, error){
		"accept the counter": func() (service.DealView, error) { return e.deals.Accept(ctx, buyer, v.ID, 1_300_000) },
		"order at the listed price": func() (service.DealView, error) {
			return e.deals.RequestCod(ctx, buyer, v.ID, 1_500_000, address, "")
		},
		"offer": func() (service.DealView, error) { return e.deals.Offer(ctx, buyer, v.ID, 1_100_000) },
	} {
		if _, err := move(); !errors.Is(err, domain.ErrSellerAway) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if d, err := e.deals.Decline(ctx, buyer, v.ID); err != nil || d.OpenPrice != nil {
		t.Fatalf("decline: %+v %v", d, err)
	}
	if d, err := e.deals.Accept(ctx, seller, ov.ID, 1_000_000); err != nil || d.Status != domain.StatusAgreed {
		t.Fatalf("the seller accepts: %+v %v", d, err)
	}

	if _, err := e.accounts.Update(ctx, seller, accountsvc.UpdateInput{ClearAway: true}); err != nil {
		t.Fatal(err)
	}
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_100_000))
}

// UC-DEAL-02 E8 · NFR-CONC-01.
func TestMoveThatLostARace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	stale, err := e.repo.Get(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	must(t)(e.deals.Offer(ctx, seller, v.ID, 1_300_000))
	deps := e.deps
	deps.Deals = staleGet{Deals: e.repo, stale: stale}
	if _, err = service.New(deps).Withdraw(ctx, buyer, v.ID); !errors.Is(err, domain.ErrChanged) {
		t.Fatalf("withdraw: %v", err)
	}
	if got := e.get(t, buyer, v.ID); got.OpenPrice == nil || got.OpenPrice.By != domain.Seller {
		t.Fatalf("%+v", got)
	}
}

// UC-DEAL-02 E4 · BR-ACC-01.
func TestOpenRefusesABlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	for _, c := range []struct{ name string }{{"seller blocks"}, {"buyer blocks"}} {
		buyer := e.person(t)
		blocker, blocked := seller, buyer
		if c.name == "buyer blocks" {
			blocker, blocked = buyer, seller
		}
		if err := e.accounts.Block(ctx, blocker, blocked); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.deals.Open(ctx, buyer, listing); !errors.Is(err, accountdomain.ErrBlocked) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestBetween(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, b, c := e.person(t), e.person(t), e.person(t)
	fromA := e.open(t, b, catalogtest.Listing(t, e.db, a))
	fromB := e.open(t, a, catalogtest.Listing(t, e.db, b))
	e.open(t, c, catalogtest.Listing(t, e.db, a))
	got, err := e.deals.Between(context.Background(), a, b)
	if err != nil || len(got) != 2 || got[0].ID != fromB.ID || got[1].ID != fromA.ID {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Role != domain.Buyer || got[1].Role != domain.Seller {
		t.Fatalf("roles %s %s", got[0].Role, got[1].Role)
	}
}

func TestBuyerSummary(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	l := catalogtest.Listing(t, e.db, seller)
	if _, ok, err := e.deals.BuyerSummary(ctx, buyer, l); err != nil || ok {
		t.Fatalf("before: %v %v", ok, err)
	}
	v := e.open(t, buyer, l)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	got, ok, err := e.deals.BuyerSummary(ctx, buyer, l)
	if err != nil || !ok || got.ID != v.ID || got.Role != domain.Buyer || got.Status != domain.StatusNegotiating ||
		got.YourTurn || got.OpenPrice == nil || got.OpenPrice.Amount != 1_000_000 || got.Listing.ID != l {
		t.Fatalf("buyer: %+v %v %v", got, ok, err)
	}
	if _, ok, err = e.deals.BuyerSummary(ctx, seller, l); err != nil || ok {
		t.Fatalf("the seller is no buyer: %v %v", ok, err)
	}
}

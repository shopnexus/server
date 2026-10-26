package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

func TestNegotiateToAgreement(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)

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
	if k := kindsOf(t, e.db, v.ID); len(k) != 3 || k[0] != "deal.moved" {
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

func TestTwoBuyersDealApart(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, one, two := e.person(t), e.person(t), e.person(t)
	listing := e.listing(t, seller)
	first, second := e.open(t, one, listing), e.open(t, two, listing)
	must(t)(e.deals.Accept(ctx, one, first.ID, 1_500_000))
	if v := e.get(t, two, second.ID); v.Status != domain.StatusNegotiating {
		t.Fatalf("%+v", v)
	}
	must(t)(e.deals.Offer(ctx, two, second.ID, 1_000_000))
}

func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := e.listing(t, seller)
	hidden := e.listing(t, seller)
	early := e.person(t)
	opened := e.open(t, early, hidden)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	suspended := e.person(t)
	e.suspend(t, suspended)

	for _, c := range []struct {
		name    string
		buyer   uuid.UUID
		listing uuid.UUID
		want    error
	}{
		{"no phone", e.emailOnly(t), listing, domain.ErrPhoneRequired},
		{"own listing", seller, listing, domain.ErrOwnListing},
		{"suspended", suspended, listing, accountdomain.ErrSuspended},
		{"hidden listing", e.person(t), hidden, domain.ErrListingUnavailable},
		{"unknown listing", e.person(t), uuid.NewV7(), catalogdomain.ErrNotFound},
	} {
		if _, _, err := e.deals.Open(ctx, c.buyer, c.listing); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	v, created, err := e.deals.Open(ctx, early, hidden)
	if err != nil || created || v.ID != opened.ID {
		t.Fatalf("an existing deal on a hidden listing: %v %v", created, err)
	}
}

func TestStrangerSeesNoDeal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	v := e.open(t, buyer, e.listing(t, seller))
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

func TestAcceptAfterThePriceChanged(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)
	v := e.open(t, buyer, listing)
	e.edit(t, listing, func(d *catalogdomain.Draft) { d.Price = 1_400_000 })
	if _, err := e.deals.Accept(ctx, buyer, v.ID, 1_500_000); !errors.Is(err, domain.ErrPriceChanged) {
		t.Fatalf("old price: %v", err)
	}
	agreed, err := e.deals.Accept(ctx, buyer, v.ID, 1_400_000)
	if err != nil || agreed.Status != domain.StatusAgreed || *agreed.AgreedPrice != 1_400_000 {
		t.Fatalf("%+v %v", agreed, err)
	}
}

func TestTakeBackOnAHiddenListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, other := e.person(t), e.person(t), e.person(t)
	listing := e.listing(t, seller)
	v, agreed := e.open(t, buyer, listing), e.open(t, other, listing)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	must(t)(e.deals.Accept(ctx, other, agreed.ID, 1_500_000))
	if _, err := e.catalog.Hide(ctx, seller, listing); err != nil {
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

func TestSuspendedCallerTakesBackOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.open(t, buyer, e.listing(t, seller))
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	e.suspend(t, buyer)
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

func TestMoveThatLostARace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.open(t, buyer, e.listing(t, seller))
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

func TestOpenRefusesABlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := e.listing(t, seller)
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
	fromA := e.open(t, b, e.listing(t, a))
	fromB := e.open(t, a, e.listing(t, b))
	e.open(t, c, e.listing(t, a))
	got, err := e.deals.Between(context.Background(), a, b)
	if err != nil || len(got) != 2 || got[0].ID != fromB.ID || got[1].ID != fromA.ID {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Role != domain.Buyer || got[1].Role != domain.Seller {
		t.Fatalf("roles %s %s", got[0].Role, got[1].Role)
	}
}

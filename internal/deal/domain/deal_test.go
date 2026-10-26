package domain_test

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// onSale is an active listing at 1,500,000 đ that takes offers.
var onSale = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingActive}

func newDeal() domain.Deal {
	return domain.Open(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), now)
}

func kinds(d *domain.Deal) []domain.MoveKind {
	out := make([]domain.MoveKind, 0, len(d.Pending()))
	for _, m := range d.Pending() {
		out = append(out, m.Kind)
	}
	return out
}

// must fails t on an error from a setup move.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOfferCounterAccept(t *testing.T) {
	t.Parallel()
	d := newDeal()
	must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	must(t, d.Offer(domain.Seller, 1_300_000, onSale, now))
	must(t, d.Accept(domain.Buyer, 1_300_000, onSale, now))
	if d.Status() != domain.StatusAgreed || d.AgreedPrice() != 1_300_000 || d.AgreedAt() == nil || d.Open() != nil {
		t.Fatalf("%+v", d.State())
	}
	want := []domain.MoveKind{domain.MoveOffer, domain.MoveCounter, domain.MoveAccept}
	if !slices.Equal(kinds(&d), want) {
		t.Fatalf("%v", kinds(&d))
	}
	if p := d.Pending(); p[2].By != domain.Buyer || p[2].Amount != 1_300_000 {
		t.Fatalf("%+v", p[2])
	}
	d.Saved(uuid.NewV7())
	if len(d.Pending()) != 0 {
		t.Fatal("Saved keeps the pending moves")
	}
}

func TestMoveRules(t *testing.T) {
	t.Parallel()
	noOffers := domain.Listing{Price: 1_500_000, Status: domain.ListingActive}
	hidden := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingHidden}
	offered := func(t *testing.T, d *domain.Deal) {
		t.Helper()
		must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	}
	countered := func(t *testing.T, d *domain.Deal) {
		t.Helper()
		offered(t, d)
		must(t, d.Offer(domain.Seller, 1_300_000, onSale, now))
	}
	agreed := func(t *testing.T, d *domain.Deal) {
		t.Helper()
		must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	}
	none := func(*testing.T, *domain.Deal) {}
	cases := []struct {
		name  string
		setup func(*testing.T, *domain.Deal)
		move  func(*domain.Deal) error
		want  error
	}{
		{"the seller offers first", none, func(d *domain.Deal) error { return d.Offer(domain.Seller, 1_200_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"the buyer offers twice", offered, func(d *domain.Deal) error { return d.Offer(domain.Buyer, 1_100_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"the seller counters twice", countered, func(d *domain.Deal) error { return d.Offer(domain.Seller, 1_400_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"the buyer accepts their own offer", offered, func(d *domain.Deal) error { return d.Accept(domain.Buyer, 1_000_000, onSale, now) }, domain.ErrPriceChanged},
		{"the seller accepts with no offer", none, func(d *domain.Deal) error { return d.Accept(domain.Seller, 1_500_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"the seller accepts their own counter", countered, func(d *domain.Deal) error { return d.Accept(domain.Seller, 1_300_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"the seller accepts a stale offer", offered, func(d *domain.Deal) error { return d.Accept(domain.Seller, 900_000, onSale, now) }, domain.ErrPriceChanged},
		{"the buyer accepts an old listed price", none, func(d *domain.Deal) error { return d.Accept(domain.Buyer, 1_400_000, onSale, now) }, domain.ErrPriceChanged},
		{"the buyer declines their own offer", offered, func(d *domain.Deal) error { return d.Decline(domain.Buyer, now) }, domain.ErrMoveNotAllowed},
		{"decline with nothing open", none, func(d *domain.Deal) error { return d.Decline(domain.Seller, now) }, domain.ErrMoveNotAllowed},
		{"the seller withdraws the buyer's offer", offered, func(d *domain.Deal) error { return d.Withdraw(domain.Seller, now) }, domain.ErrMoveNotAllowed},
		{"cancel while negotiating", offered, func(d *domain.Deal) error { return d.Cancel(domain.Buyer, now) }, domain.ErrMoveNotAllowed},
		{"offer after agreeing", agreed, func(d *domain.Deal) error { return d.Offer(domain.Buyer, 1_000_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"accept after agreeing", agreed, func(d *domain.Deal) error { return d.Accept(domain.Buyer, 1_500_000, onSale, now) }, domain.ErrMoveNotAllowed},
		{"offer on a listing without offers", none, func(d *domain.Deal) error { return d.Offer(domain.Buyer, 1_000_000, noOffers, now) }, domain.ErrMoveNotAllowed},
		{"offer on a hidden listing", none, func(d *domain.Deal) error { return d.Offer(domain.Buyer, 1_000_000, hidden, now) }, domain.ErrListingUnavailable},
		{"accept on a hidden listing", none, func(d *domain.Deal) error { return d.Accept(domain.Buyer, 1_500_000, hidden, now) }, domain.ErrListingUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := newDeal()
			c.setup(t, &d)
			before := len(d.Pending())
			if err := c.move(&d); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if len(d.Pending()) != before {
				t.Fatal("a refused move was recorded")
			}
		})
	}
}

func TestAllowedMoves(t *testing.T) {
	t.Parallel()
	d := newDeal()
	must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	must(t, d.Withdraw(domain.Buyer, now))
	must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	must(t, d.Decline(domain.Seller, now))
	must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	must(t, d.Offer(domain.Seller, 1_300_000, onSale, now))
	must(t, d.Decline(domain.Buyer, now))
	must(t, d.Offer(domain.Buyer, 1_100_000, onSale, now))
	// The buyer takes the listed price over their own open offer.
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	must(t, d.Cancel(domain.Seller, now))
	if d.Status() != domain.StatusNegotiating || d.AgreedPrice() != 0 || d.AgreedAt() != nil || d.Open() != nil {
		t.Fatalf("%+v", d.State())
	}
	fixed := newDeal()
	must(t, fixed.Accept(domain.Buyer, 1_500_000, domain.Listing{Price: 1_500_000, Status: domain.ListingActive}, now))
	odd := newDeal()
	must(t, odd.Accept(domain.Buyer, 10_500, domain.Listing{Price: 10_500, AllowOffers: true, Status: domain.ListingActive}, now))
}

func TestOfferRange(t *testing.T) {
	t.Parallel()
	at := func(price int64) domain.Listing {
		return domain.Listing{Price: price, AllowOffers: true, Status: domain.ListingActive}
	}
	cases := []struct {
		name    string
		l       domain.Listing
		offer   int64 // the buyer's open offer, 0 for none
		counter int64 // the seller's counter to it, 0 for none
		by      domain.Party
		want    domain.Range
		ok      bool
	}{
		{"buyer, listed price", at(1_500_000), 0, 0, domain.Buyer, domain.Range{Min: 750_000, Max: 1_499_000}, true},
		{"buyer, cheapest listing", at(10_000), 0, 0, domain.Buyer, domain.Range{Min: 5_000, Max: 9_000}, true},
		{"buyer, odd price", at(10_500), 0, 0, domain.Buyer, domain.Range{Min: 6_000, Max: 10_000}, true},
		{"buyer, under a counter", at(1_500_000), 1_000_000, 1_200_000, domain.Buyer, domain.Range{Min: 750_000, Max: 1_199_000}, true},
		{"seller, over an offer", at(1_500_000), 1_000_000, 0, domain.Seller, domain.Range{Min: 1_001_000, Max: 1_499_000}, true},
		{"seller, offer one step under", at(1_500_000), 1_499_000, 0, domain.Seller, domain.Range{}, false},
		{"buyer with an open offer", at(1_500_000), 1_000_000, 0, domain.Buyer, domain.Range{}, false},
		{"seller with nothing open", at(1_500_000), 0, 0, domain.Seller, domain.Range{}, false},
		{"buyer, no offers", domain.Listing{Price: 1_500_000, Status: domain.ListingActive}, 0, 0, domain.Buyer, domain.Range{}, false},
		{"buyer, paused listing", domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingPaused}, 0, 0, domain.Buyer, domain.Range{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := newDeal()
			if c.offer != 0 {
				must(t, d.Offer(domain.Buyer, c.offer, at(c.l.Price), now))
			}
			if c.counter != 0 {
				must(t, d.Offer(domain.Seller, c.counter, at(c.l.Price), now))
			}
			got, ok := d.OfferRange(c.by, c.l)
			if got != c.want || ok != c.ok {
				t.Fatalf("got %+v %v", got, ok)
			}
		})
	}
}

func TestOfferAmounts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		by      domain.Party
		amount  int64
		problem string
	}{
		{"not a multiple of 1,000", domain.Buyer, 1_000_500, "step"},
		{"under half the price", domain.Buyer, 749_000, "out_of_range"},
		{"the listed price", domain.Buyer, 1_500_000, "out_of_range"},
		{"a counter at the offer", domain.Seller, 1_000_000, "out_of_range"},
		{"a counter at the listed price", domain.Seller, 1_500_000, "out_of_range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := newDeal()
			if c.by == domain.Seller {
				must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
			}
			err := d.Offer(c.by, c.amount, onSale, now)
			if !errors.Is(err, domain.ErrInvalidAmount) || apperr.From(err).Fields["amount"] != c.problem {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestListingChanged(t *testing.T) {
	t.Parallel()
	offered := func(t *testing.T) domain.Deal {
		t.Helper()
		d := newDeal()
		must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
		d.Saved(uuid.NewV7())
		return d
	}
	cases := []struct {
		name string
		l    domain.Listing
		want domain.MoveKind // "" for no change
	}{
		{"same listing", onSale, ""},
		{"paused at the same price", domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingPaused}, ""},
		{"new price", domain.Listing{Price: 1_400_000, AllowOffers: true, Status: domain.ListingActive}, domain.MovePriceChanged},
		{"new price while paused", domain.Listing{Price: 1_400_000, AllowOffers: true, Status: domain.ListingPaused}, domain.MovePriceChanged},
		{"offers off", domain.Listing{Price: 1_500_000, Status: domain.ListingActive}, domain.MoveOffersClosed},
		{"hidden", domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingHidden}, domain.MoveListingClosed},
		{"gone", domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingGone}, domain.MoveListingClosed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := offered(t)
			changed := d.ListingChanged(c.l, now)
			if changed != (c.want != "") {
				t.Fatalf("changed %v", changed)
			}
			if !changed {
				if d.Open() == nil {
					t.Fatal("the open price went")
				}
				return
			}
			p := d.Pending()
			if d.Open() != nil || len(p) != 1 || p[0].Kind != c.want || p[0].By != domain.System {
				t.Fatalf("%+v %+v", d.State(), p)
			}
			if c.want == domain.MovePriceChanged && p[0].Amount != c.l.Price {
				t.Fatalf("amount %d", p[0].Amount)
			}
			d.Saved(uuid.NewV7())
			if d.ListingChanged(c.l, now) {
				t.Fatal("a second call changed the deal again")
			}
		})
	}
}

func TestListingGoneEndsTheAgreement(t *testing.T) {
	t.Parallel()
	d := newDeal()
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	d.Saved(uuid.NewV7())
	if d.ListingChanged(domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingHidden}, now) {
		t.Fatal("a hidden listing ended the agreement")
	}
	if !d.ListingChanged(domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingGone}, now) ||
		d.Status() != domain.StatusNegotiating || d.AgreedPrice() != 0 || kinds(&d)[0] != domain.MoveListingClosed {
		t.Fatalf("%+v %v", d.State(), kinds(&d))
	}
}

func TestPartyOf(t *testing.T) {
	t.Parallel()
	buyer, seller := uuid.NewV7(), uuid.NewV7()
	d := domain.Open(uuid.NewV7(), uuid.NewV7(), buyer, seller, now)
	if p, ok := d.PartyOf(buyer); p != domain.Buyer || !ok {
		t.Fatal("buyer")
	}
	if p, ok := d.PartyOf(seller); p != domain.Seller || !ok {
		t.Fatal("seller")
	}
	if _, ok := d.PartyOf(uuid.NewV7()); ok {
		t.Fatal("a stranger is a party")
	}
}

// A listing change voids the open price at once, before ListingChanged
// records it.
func TestStaleOpenPrice(t *testing.T) {
	t.Parallel()
	t.Run("offers off", func(t *testing.T) {
		t.Parallel()
		d := newDeal()
		must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
		off := domain.Listing{Price: 1_500_000, Status: domain.ListingActive}
		if _, ok := d.OfferRange(domain.Seller, off); ok {
			t.Fatal("the seller may counter an offer the listing no longer takes")
		}
		if err := d.Offer(domain.Seller, 1_200_000, off, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("counter: %v", err)
		}
		if err := d.Accept(domain.Seller, 1_000_000, off, now); !errors.Is(err, domain.ErrPriceChanged) {
			t.Fatalf("seller accepts: %v", err)
		}
		if d.OpenFor(off) != nil {
			t.Fatal("the view shows a voided offer")
		}
		must(t, d.Accept(domain.Buyer, 1_500_000, off, now))
	})
	t.Run("price drop", func(t *testing.T) {
		t.Parallel()
		d := newDeal()
		must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
		must(t, d.Offer(domain.Seller, 1_400_000, onSale, now))
		cheaper := domain.Listing{Price: 1_200_000, AllowOffers: true, Status: domain.ListingActive}
		if err := d.Accept(domain.Buyer, 1_400_000, cheaper, now); !errors.Is(err, domain.ErrPriceChanged) {
			t.Fatalf("accept the old counter: %v", err)
		}
		if r, ok := d.OfferRange(domain.Buyer, cheaper); !ok || r != (domain.Range{Min: 600_000, Max: 1_199_000}) {
			t.Fatalf("range %+v %v", r, ok)
		}
		must(t, d.Offer(domain.Buyer, 1_100_000, cheaper, now))
		if o := d.OpenFor(cheaper); o == nil || o.By != domain.Buyer || o.Amount != 1_100_000 {
			t.Fatalf("%+v", o)
		}
	})
}

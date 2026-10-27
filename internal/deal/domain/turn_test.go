package domain_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func TestTurnOf(t *testing.T) {
	t.Parallel()
	cheaper := domain.Listing{Price: 1_200_000, AllowOffers: true, Status: domain.ListingActive}
	cases := []struct {
		name          string
		deal          func(*testing.T) domain.Deal
		listing       domain.Listing
		buyer, seller bool
		active        bool
	}{
		{"nothing open", func(*testing.T) domain.Deal { return newDeal() }, onSale, false, false, true},
		{"the buyer's offer", func(t *testing.T) domain.Deal {
			t.Helper()
			d := newDeal()
			must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
			return d
		}, onSale, false, true, true},
		{"the seller's counter", func(t *testing.T) domain.Deal {
			t.Helper()
			d := newDeal()
			must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
			must(t, d.Offer(domain.Seller, 1_300_000, onSale, now))
			return d
		}, onSale, true, false, true},
		{"an offer a new listed price voided", func(t *testing.T) domain.Deal {
			t.Helper()
			d := newDeal()
			must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
			return d
		}, cheaper, false, false, true},
		{"agreed without an order", agreed, onSale, true, false, true},
		{"agreed on a listing reserved for another order", agreed, reserved, false, false, true},
		{"agreed on a hidden listing", agreed, domain.Listing{Price: 1_500_000, Status: domain.ListingHidden}, false, false, true},
		{"a requested order", requested, onSale, false, true, true},
		{"a shipped order", shipped, reserved, false, true, true},
		{"a cancelled order", func(t *testing.T) domain.Deal {
			t.Helper()
			d := requested(t)
			must(t, d.Cancel(domain.Buyer, now))
			return d
		}, onSale, false, false, true},
		{"agreed again after a return", func(t *testing.T) domain.Deal {
			t.Helper()
			d := shipped(t)
			must(t, d.Return(domain.Seller, now))
			must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
			return d
		}, onSale, true, false, true},
		{"sold", func(t *testing.T) domain.Deal {
			t.Helper()
			d := shipped(t)
			must(t, d.Deliver(domain.Seller, now))
			return d
		}, gone, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := c.deal(t)
			if got := d.TurnOf(domain.Buyer, c.listing); got != c.buyer {
				t.Errorf("buyer's turn %v, want %v", got, c.buyer)
			}
			if got := d.TurnOf(domain.Seller, c.listing); got != c.seller {
				t.Errorf("seller's turn %v, want %v", got, c.seller)
			}
			if d.Active() != c.active {
				t.Errorf("active %v, want %v", d.Active(), c.active)
			}
		})
	}
}

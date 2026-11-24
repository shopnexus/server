package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/deal/domain"
)

func TestPartyLeft(t *testing.T) {
	t.Parallel()
	req := pending(t)
	if !req.PartyLeft(now) || req.Order().Status != domain.OrderCancelled || req.Order().CancelledBy != domain.System ||
		req.Order().CancelReason != domain.CancelListingClosed || req.Status() != domain.StatusNegotiating {
		t.Fatalf("pending: %+v", req.State())
	}

	settled := agreed(t)
	if !settled.PartyLeft(now) || settled.Status() != domain.StatusNegotiating || settled.AgreedPrice() != 0 {
		t.Fatalf("agreed: %+v", settled.State())
	}

	offered := newDeal()
	must(t, offered.Offer(domain.Buyer, 1_000_000, onSale, now))
	if !offered.PartyLeft(now) || offered.OpenPrice() != nil {
		t.Fatalf("offered: %+v", offered.State())
	}
	if offered.PartyLeft(now) {
		t.Fatal("a second PartyLeft changed the deal")
	}

	ship := shipped(t)
	if ship.PartyLeft(now) {
		t.Fatal("PartyLeft touched a shipped order")
	}
}

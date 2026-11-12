package domain_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func TestPartyLeft(t *testing.T) {
	t.Parallel()
	req := pending(t)
	if !req.PartyLeft(now) || req.Order().Status != domain.OrderCancelled || req.Order().CancelledBy != domain.System ||
		req.Order().CancelReason != domain.CancelListingClosed || req.Status() != domain.StatusNegotiating {
		t.Fatalf("pending: %+v", req.State())
	}

	agreed := newDeal()
	must(t, agreed.Accept(domain.Buyer, 1_500_000, onSale, now))
	if !agreed.PartyLeft(now) || agreed.Status() != domain.StatusNegotiating || agreed.AgreedPrice() != 0 {
		t.Fatalf("agreed: %+v", agreed.State())
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

package service

import (
	"context"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/page"
)

// OffersScreen is the OffersScreen schema in api/schemas/screen_offers.yaml.
type OffersScreen struct {
	Items      []OffersScreenItem `json:"items"`
	NextCursor *uuid.UUID         `json:"next_cursor,omitempty"`
}

// OffersScreenItem is the OffersScreenItem schema in
// api/schemas/screen_offers.yaml.
type OffersScreenItem struct {
	Deal  dealsvc.DealView   `json:"deal"`
	Buyer chatsvc.PersonView `json:"buyer"`
}

// Offers reads a page of the buyers' open offers on seller's listings,
// then their buyers.
func (s *Screen) Offers(ctx context.Context, seller uuid.UUID, p page.Request) (OffersScreen, error) {
	offers, err := s.d.Deals.OpenOffers(ctx, seller, p)
	if err != nil {
		return OffersScreen{}, fmt.Errorf("read open offers: %w", err)
	}
	buyers := make([]uuid.UUID, 0, len(offers.Items))
	for i := range offers.Items {
		buyers = append(buyers, offers.Items[i].BuyerID)
	}
	people, err := s.d.Accounts.GetMany(ctx, buyers)
	if err != nil {
		return OffersScreen{}, fmt.Errorf("read buyers: %w", err)
	}
	out := OffersScreen{Items: make([]OffersScreenItem, 0, len(offers.Items)), NextCursor: offers.NextCursor}
	for i := range offers.Items {
		d := offers.Items[i]
		out.Items = append(out.Items, OffersScreenItem{Deal: d, Buyer: chatsvc.PersonFromAccount(d.BuyerID, people[d.BuyerID])})
	}
	return out, nil
}

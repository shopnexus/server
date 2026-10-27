package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// ListingScreen is the ListingScreen schema in api/schemas/screen_listing.yaml.
type ListingScreen struct {
	Listing catalogsvc.ListingView `json:"listing"`
	Seller  SellerSummary          `json:"seller"`
	Viewer  *ListingViewer         `json:"viewer,omitempty"`
}

// SellerSummary is the SellerSummary schema in api/schemas/screen_listing.yaml.
type SellerSummary struct {
	ID          uuid.UUID       `json:"id"`
	DisplayName string          `json:"display_name"`
	AvatarURL   string          `json:"avatar_url,omitempty"`
	JoinedAt    time.Time       `json:"joined_at"`
	Rating      trustsvc.Rating `json:"rating"`
	Sold        int             `json:"sold"`
}

// ListingViewer is the ListingViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type ListingViewer struct {
	Saved          bool                 `json:"saved"`
	Deal           *dealsvc.DealSummary `json:"deal,omitempty"`
	ConversationID uuid.UUID            `json:"conversation_id,omitzero"`
}

// Listing reads the listing signed out, so its seller too gets
// catalogdomain.ErrNotFound for one that is not active or reserved. A
// reader who is not the seller also gets their save, deal and
// conversation with the seller.
func (s *Screen) Listing(ctx context.Context, reader *uuid.UUID, id uuid.UUID) (ListingScreen, error) {
	l, err := s.catalog.Get(ctx, nil, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the listing: %w", err)
	}
	seller, err := s.trust.Seller(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	out := ListingScreen{Listing: l, Seller: SellerSummary{
		ID: seller.ID, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, JoinedAt: seller.JoinedAt,
		Rating: seller.Rating, Sold: seller.Sold,
	}}
	if reader == nil || *reader == l.SellerID {
		return out, nil
	}
	saved, err := s.saves.IsSaved(ctx, *reader, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the save: %w", err)
	}
	deal, hasDeal, err := s.deals.BuyerSummary(ctx, *reader, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the deal: %w", err)
	}
	conversation, err := s.chats.ConversationWith(ctx, *reader, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the conversation: %w", err)
	}
	out.Viewer = &ListingViewer{Saved: saved, ConversationID: conversation}
	if hasDeal {
		out.Viewer.Deal = &deal
	}
	return out, nil
}

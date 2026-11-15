package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// ListingScreen is the ListingScreen schema in api/schemas/screen_listing.yaml.
// DistanceKm is nil without a position; it sits beside the listing because
// it depends on the request.
type ListingScreen struct {
	Listing    catalogsvc.ListingView `json:"listing"`
	DistanceKm *int                   `json:"distance_km,omitempty"`
	Seller     SellerSummary          `json:"seller"`
	Viewer     *ListingViewer         `json:"viewer,omitempty"`
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
// catalogdomain.ErrNotFound unless it is active or reserved, then its
// distance from near. A viewer who is not the seller also gets their save,
// deal and conversation with the seller.
func (s *Screen) Listing(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, near catalogdomain.Near) (ListingScreen, error) {
	l, err := s.catalog.Get(ctx, nil, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the listing: %w", err)
	}
	distance, err := s.catalog.Distance(ctx, near, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("measure the distance: %w", err)
	}
	seller, err := s.accounts.Get(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	rating, err := s.trust.Rating(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	counts, err := s.catalog.SellerCounts(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("count the listings: %w", err)
	}
	out := ListingScreen{Listing: l, DistanceKm: distance, Seller: SellerSummary{
		ID: l.SellerID, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, JoinedAt: seller.CreatedAt,
		Rating: rating, Sold: counts.Sold,
	}}
	if viewer == nil || *viewer == l.SellerID {
		return out, nil
	}
	saved, err := s.discovery.IsSaved(ctx, *viewer, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the save: %w", err)
	}
	deal, hasDeal, err := s.deals.BuyerSummary(ctx, *viewer, id)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the deal: %w", err)
	}
	conversation, err := s.chats.ConversationWith(ctx, *viewer, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the conversation: %w", err)
	}
	out.Viewer = &ListingViewer{Saved: saved, ConversationID: conversation}
	if hasDeal {
		out.Viewer.Deal = &deal
	}
	return out, nil
}

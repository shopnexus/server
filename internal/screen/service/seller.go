package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/page"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// SellerScreen is the SellerScreen schema in api/schemas/screen_listing.yaml.
type SellerScreen struct {
	Seller   SellerProfile                     `json:"seller"`
	Listings page.Page[catalogsvc.ListingView] `json:"listings"`
	Reviews  page.Page[trustsvc.ReviewView]    `json:"reviews"`
	Viewer   *SellerViewer                     `json:"viewer,omitempty"`
}

// SellerProfile is the SellerProfile schema in
// api/schemas/screen_listing.yaml.
type SellerProfile struct {
	ID          uuid.UUID       `json:"id"`
	DisplayName string          `json:"display_name"`
	AvatarURL   string          `json:"avatar_url,omitempty"`
	Bio         string          `json:"bio"`
	JoinedAt    time.Time       `json:"joined_at"`
	Rating      trustsvc.Rating `json:"rating"`
	Sold        int             `json:"sold"`
	OnSale      int             `json:"on_sale"`
}

// SellerViewer is the SellerViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type SellerViewer struct {
	Blocked        bool      `json:"blocked"`
	ConversationID uuid.UUID `json:"conversation_id,omitzero"`
}

// Seller reads the seller's profile and the first page, of the default
// size, of their public listings and of their reviews. An unknown account is
// accountdomain.ErrAccountNotFound. A viewer who is not the seller also
// gets whether a block stands between the two and their conversation.
func (s *Screen) Seller(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (SellerScreen, error) {
	seller, err := s.accounts.Get(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	counts, err := s.catalog.SellerCounts(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("count the listings: %w", err)
	}
	rating, err := s.trust.Rating(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	listings, err := s.catalog.SellerListings(ctx, id, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the listings: %w", err)
	}
	reviews, err := s.trust.SellerReviews(ctx, id, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the reviews: %w", err)
	}
	out := SellerScreen{
		Seller: SellerProfile{
			ID: id, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, Bio: seller.Bio, JoinedAt: seller.CreatedAt,
			Rating: rating, Sold: counts.Sold, OnSale: counts.OnSale,
		},
		Listings: listings, Reviews: reviews,
	}
	if viewer == nil || *viewer == id {
		return out, nil
	}
	blocked, err := s.accounts.Blocked(ctx, *viewer, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the blocks: %w", err)
	}
	conversation, err := s.chats.ConversationWith(ctx, *viewer, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the conversation: %w", err)
	}
	out.Viewer = &SellerViewer{Blocked: blocked, ConversationID: conversation}
	return out, nil
}

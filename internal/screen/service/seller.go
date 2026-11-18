package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/page"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// SellerScreen is the SellerScreen schema in api/schemas/screen_listing.yaml.
type SellerScreen struct {
	Seller   SellerProfile                      `json:"seller"`
	Listings page.Page[discoverysvc.SearchItem] `json:"listings"`
	Reviews  page.Page[trustsvc.ReviewView]     `json:"reviews"`
	Viewer   *SellerViewer                      `json:"viewer,omitempty"`
}

// SellerProfile is the SellerProfile schema in
// api/schemas/screen_listing.yaml.
type SellerProfile struct {
	ID           uuid.UUID       `json:"id"`
	DisplayName  string          `json:"display_name"`
	AvatarURL    string          `json:"avatar_url,omitempty"`
	Bio          string          `json:"bio"`
	JoinedAt     time.Time       `json:"joined_at"`
	LastActiveAt *time.Time      `json:"last_active_at,omitempty"`
	Rating       trustsvc.Rating `json:"rating"`
	Sold         int             `json:"sold"`
	OnSale       int             `json:"on_sale"`
	Followers    int             `json:"followers"`
}

// SellerViewer is the SellerViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type SellerViewer struct {
	Blocked        bool      `json:"blocked"`
	Following      bool      `json:"following"`
	ConversationID uuid.UUID `json:"conversation_id,omitzero"`
}

// Seller reads the seller's profile and the first page, of the default
// size, of their public listings as search items, each with its distance
// from near and whether the viewer saved it, and of their reviews. An
// unknown account is accountdomain.ErrAccountNotFound. A viewer who is not
// the seller also gets whether a block stands between the two, whether
// they follow the seller, and their conversation.
func (s *Screen) Seller(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, near catalogdomain.Near) (SellerScreen, error) {
	seller, err := s.accounts.Get(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	if seller.Status == accountdomain.StatusDeleted {
		return SellerScreen{}, accountdomain.ErrAccountNotFound
	}
	counts, err := s.catalog.SellerCounts(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("count the listings: %w", err)
	}
	rating, err := s.trust.Rating(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	followers, err := s.accounts.FollowerCount(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("count the followers: %w", err)
	}
	listings, err := s.discovery.SellerListings(ctx, viewer, id, near, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the listings: %w", err)
	}
	reviews, err := s.trust.SellerReviews(ctx, id, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the reviews: %w", err)
	}
	out := SellerScreen{
		Seller: SellerProfile{
			ID: id, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, Bio: seller.Bio, JoinedAt: seller.CreatedAt, LastActiveAt: seller.LastActiveAt,
			Rating: rating, Sold: counts.Sold, OnSale: counts.Public, Followers: followers,
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
	following, err := s.accounts.Following(ctx, *viewer, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the follow: %w", err)
	}
	conversation, err := s.chats.ConversationWith(ctx, *viewer, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the conversation: %w", err)
	}
	out.Viewer = &SellerViewer{Blocked: blocked, Following: following, ConversationID: conversation}
	return out, nil
}

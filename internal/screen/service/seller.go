package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/page"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// SellerScreen is the SellerScreen schema in api/schemas/screen_listing.yaml.
type SellerScreen struct {
	Seller   SellerProfile                      `json:"seller"`
	Listings page.Page[discoverysvc.SearchItem] `json:"listings"`
	Reviews  page.Page[trustsvc.ReviewView]     `json:"reviews"`
	// BuyerReviews are the person's reviews as a buyer, the Khi mua tab.
	BuyerReviews page.Page[trustsvc.ReviewView] `json:"buyer_reviews"`
	Viewer       *SellerViewer                  `json:"viewer,omitempty"`
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
	BuyerRating  trustsvc.Rating `json:"buyer_rating"`
	Sold         int             `json:"sold"`
	OnSale       int             `json:"on_sale"`
	Followers    int             `json:"followers"`
	// ReplyTime is "" when the seller has too few answers to tell.
	ReplyTime chatdomain.ReplyTime `json:"reply_time,omitempty"`
}

// SellerViewer is the SellerViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type SellerViewer struct {
	Blocked        bool      `json:"blocked"`
	Following      bool      `json:"following"`
	ConversationID uuid.UUID `json:"conversation_id,omitzero"`
}

// Seller reads the seller's profile with how soon they usually answer, and
// the first page, of the default
// size, of their public listings as search items, each with its distance
// from near and whether the viewer saved it, and of their reviews as a
// seller and as a buyer, with both ratings. An
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
	rating, err := s.trust.Rating(ctx, id, trustdomain.RoleSeller)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	buyerRating, err := s.trust.Rating(ctx, id, trustdomain.RoleBuyer)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the buyer rating: %w", err)
	}
	followers, err := s.accounts.FollowerCount(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("count the followers: %w", err)
	}
	replyTime, err := s.chats.ReplyTime(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the reply time: %w", err)
	}
	listings, err := s.discovery.SellerListings(ctx, viewer, id, near, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the listings: %w", err)
	}
	reviews, err := s.trust.Reviews(ctx, viewer, id, trustdomain.RoleSeller, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the reviews: %w", err)
	}
	buyerReviews, err := s.trust.Reviews(ctx, viewer, id, trustdomain.RoleBuyer, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the buyer reviews: %w", err)
	}
	out := SellerScreen{
		Seller: SellerProfile{
			ID: id, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, Bio: seller.Bio, JoinedAt: seller.CreatedAt, LastActiveAt: seller.LastActiveAt,
			Rating: rating, BuyerRating: buyerRating, Sold: counts.Sold, OnSale: counts.Public, Followers: followers,
			ReplyTime: replyTime,
		},
		Listings: listings, Reviews: reviews, BuyerReviews: buyerReviews,
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

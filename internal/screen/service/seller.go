package service

import (
	"context"
	"fmt"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/page"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// SellerScreen is the SellerScreen schema in api/schemas/screen_listing.yaml.
type SellerScreen struct {
	Seller   trustsvc.SellerView               `json:"seller"`
	Listings page.Page[catalogsvc.ListingView] `json:"listings"`
	Reviews  trustsvc.SellerReviews            `json:"reviews"`
	Viewer   *SellerViewer                     `json:"viewer,omitempty"`
}

// SellerViewer is the SellerViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type SellerViewer struct {
	Blocked        bool      `json:"blocked"`
	ConversationID uuid.UUID `json:"conversation_id,omitzero"`
}

// Seller reads the seller's profile and the first page, of the default
// size, of their public listings and of their reviews. An unknown account
// is accountdomain.ErrAccountNotFound. A reader who is not the seller also
// gets whether a block stands between the two and their conversation.
func (s *Screen) Seller(ctx context.Context, reader *uuid.UUID, id uuid.UUID) (SellerScreen, error) {
	seller, err := s.trust.Seller(ctx, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	listings, err := s.catalog.SellerListings(ctx, id, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the listings: %w", err)
	}
	reviews, err := s.trust.SellerReviews(ctx, id, page.Request{})
	if err != nil {
		return SellerScreen{}, fmt.Errorf("list the reviews: %w", err)
	}
	out := SellerScreen{Seller: seller, Listings: listings, Reviews: reviews}
	if reader == nil || *reader == id {
		return out, nil
	}
	blocked, err := s.accounts.Blocked(ctx, *reader, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read blocks: %w", err)
	}
	conversation, err := s.chats.ConversationWith(ctx, *reader, id)
	if err != nil {
		return SellerScreen{}, fmt.Errorf("read the conversation: %w", err)
	}
	out.Viewer = &SellerViewer{Blocked: blocked, ConversationID: conversation}
	return out, nil
}

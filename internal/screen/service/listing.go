package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
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
	// ReplyTime is "" when the seller has too few answers to tell.
	ReplyTime chatdomain.ReplyTime `json:"reply_time,omitempty"`
}

// ListingViewer is the ListingViewer schema in
// api/schemas/screen_listing.yaml. ConversationID is zero when there is
// none.
type ListingViewer struct {
	Saved          bool                 `json:"saved"`
	Deal           *dealsvc.DealSummary `json:"deal,omitempty"`
	ConversationID uuid.UUID            `json:"conversation_id,omitzero"`
}

// ListingRead is what a read of the listing view asks for.
type ListingRead struct {
	// Ref is the listing's ID or the slug of its web address.
	Ref  string
	Near catalogdomain.Near
	// Prefetch marks a browser's speculative read, which counts no view:
	// the person may never open the page.
	Prefetch bool
}

// Listing reads the listing signed out, so its seller too gets
// catalogdomain.ErrNotFound unless it is active or reserved, then its
// distance from in.Near. A viewer who is not the seller also gets their
// save, deal and conversation with the seller, and counts a view of the
// listing unless the read is a prefetch. The count is best effort: a failed
// count is logged, and the view answers all the same.
func (s *Screen) Listing(ctx context.Context, viewer *uuid.UUID, in ListingRead) (ListingScreen, error) {
	id, err := uuid.Parse(in.Ref)
	if err != nil {
		ids, findErr := s.catalog.IDsBySlug(ctx, []string{in.Ref})
		if findErr != nil {
			return ListingScreen{}, fmt.Errorf("find the listing: %w", findErr)
		}
		var ok bool
		if id, ok = ids[in.Ref]; !ok {
			return ListingScreen{}, catalogdomain.ErrNotFound
		}
	}
	l, distance, err := s.catalog.GetNear(ctx, nil, id, in.Near)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the listing: %w", err)
	}
	seller, err := s.accounts.Get(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	rating, err := s.trust.Rating(ctx, l.SellerID, trustdomain.RoleSeller)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	counts, err := s.catalog.SellerCounts(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("count the listings: %w", err)
	}
	replyTime, err := s.chats.ReplyTime(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the reply time: %w", err)
	}
	out := ListingScreen{Listing: l, DistanceKm: distance, Seller: SellerSummary{
		ID: l.SellerID, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, JoinedAt: seller.CreatedAt,
		Rating: rating, Sold: counts.Sold, ReplyTime: replyTime,
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
	if in.Prefetch {
		return out, nil
	}
	if err = s.discovery.Viewed(ctx, *viewer, id); err != nil {
		s.log.WarnContext(ctx, "a listing view went uncounted", "listing", id, "err", err)
	}
	return out, nil
}

package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
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
	// PhoneVerified is whether the seller has a phone identity; the number
	// stays out of the view.
	PhoneVerified bool `json:"phone_verified"`
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
	id, err := s.listingID(ctx, in.Ref)
	if err != nil {
		return ListingScreen{}, err
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
	phoneVerified, err := s.accounts.HasPhone(ctx, l.SellerID)
	if err != nil {
		return ListingScreen{}, fmt.Errorf("read the phone identity: %w", err)
	}
	out := ListingScreen{Listing: l, DistanceKm: distance, Seller: SellerSummary{
		ID: l.SellerID, DisplayName: seller.DisplayName, AvatarURL: seller.AvatarURL, JoinedAt: seller.CreatedAt,
		Rating: rating, Sold: counts.Sold, PhoneVerified: phoneVerified, ReplyTime: replyTime,
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

// SoldListingScreen is the SoldListingScreen schema in
// api/schemas/screen_listing.yaml.
type SoldListingScreen struct {
	Listing SoldListing `json:"listing"`
}

// SoldListing is the SoldListing schema in api/schemas/screen_listing.yaml.
type SoldListing struct {
	ID           uuid.UUID               `json:"id"`
	Slug         string                  `json:"slug"`
	Title        string                  `json:"title"`
	Price        int64                   `json:"price"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	Location     catalogsvc.Location     `json:"location"`
	Photos       []catalogsvc.PhotoView  `json:"photos"`
	SoldAt       time.Time               `json:"sold_at"`
}

// SoldListing reads what anyone still sees of a sold listing. A listing in
// another status, or one whose seller deleted their account, is
// catalogdomain.ErrNotFound: erasure keeps a sold listing for the buyer's
// order, not for the public.
func (s *Screen) SoldListing(ctx context.Context, ref string) (SoldListingScreen, error) {
	id, err := s.listingID(ctx, ref)
	if err != nil {
		return SoldListingScreen{}, err
	}
	l, err := s.catalog.GetSold(ctx, id)
	if err != nil {
		return SoldListingScreen{}, fmt.Errorf("read the listing: %w", err)
	}
	seller, err := s.accounts.Get(ctx, l.SellerID)
	if err != nil {
		return SoldListingScreen{}, fmt.Errorf("read the seller: %w", err)
	}
	if seller.Status == accountdomain.StatusDeleted {
		return SoldListingScreen{}, catalogdomain.ErrNotFound
	}
	return SoldListingScreen{Listing: SoldListing{
		ID: l.ID, Slug: l.Slug, Title: l.Title, Price: l.Price, CategoryCode: l.CategoryCode, Condition: l.Condition,
		Location: l.Location, Photos: l.Photos, SoldAt: *l.SoldAt,
	}}, nil
}

// listingID is the listing ref names: an ID, or the slug of its web
// address, found by the code that ends it.
func (s *Screen) listingID(ctx context.Context, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	ids, err := s.catalog.IDsBySlug(ctx, []string{ref})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("find the listing: %w", err)
	}
	id, ok := ids[ref]
	if !ok {
		return uuid.UUID{}, catalogdomain.ErrNotFound
	}
	return id, nil
}

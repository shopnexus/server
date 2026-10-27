package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// SavedView is the SavedListing schema in api/schemas/discovery.yaml.
type SavedView struct {
	ID         uuid.UUID    `json:"id"`
	ListingID  uuid.UUID    `json:"listing_id"`
	SavedPrice int64        `json:"saved_price"`
	SavedAt    time.Time    `json:"saved_at"`
	Listing    SavedListing `json:"listing"`
}

// SavedListing is the saved listing as it is now; a hidden or deleted one
// shows its status alone, since its seller took it down.
type SavedListing struct {
	Title         string               `json:"title,omitempty"`
	Price         *int64               `json:"price,omitempty"`
	CoverThumbURL string               `json:"cover_thumb_url,omitempty"`
	Status        catalogdomain.Status `json:"status"`
}

func (d *Discovery) Save(ctx context.Context, account, listing uuid.UUID) error {
	got, err := d.catalog.Summaries(ctx, []uuid.UUID{listing})
	if err != nil {
		return fmt.Errorf("read listing: %w", err)
	}
	l, ok := got[listing]
	switch {
	case !ok:
		return catalogdomain.ErrNotFound
	case l.SellerID == account:
		return domain.ErrOwnListing
	case l.Status != catalogdomain.StatusActive && l.Status != catalogdomain.StatusReserved:
		return domain.ErrListingUnavailable
	default:
	}
	if err = d.saves.Save(ctx, uuid.NewV7(), account, listing, l.Price, d.now()); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

func (d *Discovery) Unsave(ctx context.Context, account, listing uuid.UUID) error {
	if err := d.saves.Remove(ctx, account, listing); err != nil {
		return fmt.Errorf("unsave: %w", err)
	}
	return nil
}

func (d *Discovery) IsSaved(ctx context.Context, account, listing uuid.UUID) (bool, error) {
	saved, err := d.saves.Has(ctx, account, listing)
	if err != nil {
		return false, fmt.Errorf("read save: %w", err)
	}
	return saved, nil
}

// Saved reads one page of saves and their listings in one call.
func (d *Discovery) Saved(ctx context.Context, account uuid.UUID, p page.Request) (page.Page[SavedView], error) {
	rows, err := d.saves.List(ctx, account, p)
	if err != nil {
		return page.Page[SavedView]{}, fmt.Errorf("list saves: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ListingID)
	}
	listings, err := d.catalog.Summaries(ctx, ids)
	if err != nil {
		return page.Page[SavedView]{}, fmt.Errorf("read listings: %w", err)
	}
	items := make([]SavedView, 0, len(rows))
	for _, r := range rows {
		l := listings[r.ListingID]
		shown := SavedListing{Status: l.Status}
		if l.Status != catalogdomain.StatusHidden && l.Status != catalogdomain.StatusDeleted {
			shown.Title, shown.Price, shown.CoverThumbURL = l.Title, new(l.Price), l.CoverThumbURL
		}
		items = append(items, SavedView{ID: r.ID, ListingID: r.ListingID, SavedPrice: r.Price, SavedAt: r.At, Listing: shown})
	}
	return page.Of(items, p, func(v SavedView) uuid.UUID { return v.ID }), nil
}

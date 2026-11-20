package service

import (
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// SavedView is the SavedListing schema in api/schemas/discovery.yaml.
type SavedView struct {
	ID         uuid.UUID    `json:"id"`
	ListingID  uuid.UUID    `json:"listing_id"`
	SavedPrice int64        `json:"saved_price"`
	SavedAt    time.Time    `json:"saved_at"`
	Listing    SavedListing `json:"listing"`
	// AlertBelow is the save's own alert level, absent for none.
	AlertBelow *int64 `json:"alert_below,omitempty"`
}

// SavedListing is the saved listing as it is now; a hidden or deleted one
// shows its status alone, since its seller took it down.
type SavedListing struct {
	Slug          string                  `json:"slug,omitempty"`
	Title         string                  `json:"title,omitempty"`
	Price         *int64                  `json:"price,omitempty"`
	CoverThumbURL string                  `json:"cover_thumb_url,omitempty"`
	CategoryCode  string                  `json:"category_code,omitempty"`
	Condition     catalogdomain.Condition `json:"condition,omitempty"`
	// Commune is the name of the listing's commune.
	Commune string               `json:"commune,omitempty"`
	Status  catalogdomain.Status `json:"status"`
}

func (d *Discovery) Save(ctx context.Context, account, listing uuid.UUID) error {
	l, ok, err := d.catalog.Summary(ctx, listing)
	if err != nil {
		return fmt.Errorf("read listing: %w", err)
	}
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
	saved, err := d.saves.SavedAmong(ctx, account, []uuid.UUID{listing})
	if err != nil {
		return false, fmt.Errorf("read save: %w", err)
	}
	return saved[listing], nil
}

// takenDown are the statuses in which a saved listing shows its status
// alone, since its seller took it down.
var takenDown = []catalogdomain.Status{catalogdomain.StatusHidden, catalogdomain.StatusDeleted}

// ended are the statuses whose saves list after the others: the listing
// sold, or its seller took it down.
var ended = []catalogdomain.Status{catalogdomain.StatusSold, catalogdomain.StatusHidden, catalogdomain.StatusDeleted}

// Saved reads one page of saves, those of listings still on sale first,
// their listings and the communes of the listings it shows, one call each.
func (d *Discovery) Saved(ctx context.Context, account uuid.UUID, p page.Request) (page.Page[SavedView], error) {
	rows, err := d.saves.List(ctx, account, ended, p)
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
	codes := make([]string, 0, len(listings))
	for id := range listings {
		if l := listings[id]; !slices.Contains(takenDown, l.Status) {
			codes = append(codes, l.CommuneCode)
		}
	}
	places, err := d.catalog.Places(ctx, codes)
	if err != nil {
		return page.Page[SavedView]{}, fmt.Errorf("read communes: %w", err)
	}
	items := make([]SavedView, 0, len(rows))
	for _, r := range rows {
		l := listings[r.ListingID]
		shown := SavedListing{Status: l.Status}
		if !slices.Contains(takenDown, l.Status) {
			shown.Slug, shown.Title, shown.Price, shown.CoverThumbURL = l.Slug, l.Title, new(l.Price), l.CoverThumbURL
			shown.CategoryCode, shown.Condition = l.CategoryCode, l.Condition
			shown.Commune = places[l.CommuneCode].Commune.DisplayName
		}
		items = append(items, SavedView{ID: r.ID, ListingID: r.ListingID, SavedPrice: r.Price, SavedAt: r.At, Listing: shown, AlertBelow: r.AlertBelow})
	}
	return page.Of(items, p, func(v SavedView) uuid.UUID { return v.ID }), nil
}

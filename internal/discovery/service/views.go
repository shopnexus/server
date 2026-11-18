package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/discovery/domain"
)

// ListingStats is what discovery knows of how a listing does: its views and
// saves, all of them and those of a recent period.
type ListingStats struct {
	Views, ViewsSince int
	Saves, SavesSince int
}

// Viewed counts viewer's opening of listing once per UTC day
// (domain.ViewDay). The listing's seller and a deleted account count
// nothing.
func (d *Discovery) Viewed(ctx context.Context, viewer, listing uuid.UUID) error {
	if err := d.views.Record(ctx, listing, viewer, domain.ViewDay(d.now())); err != nil {
		return fmt.Errorf("count the view: %w", err)
	}
	return nil
}

// ListingStats reads the views and the saves of the listings among ids in
// one query each.
func (d *Discovery) ListingStats(ctx context.Context, ids []uuid.UUID, since time.Time) (map[uuid.UUID]ListingStats, error) {
	views, err := d.views.Counts(ctx, ids, domain.ViewDay(since))
	if err != nil {
		return nil, fmt.Errorf("count views: %w", err)
	}
	saves, err := d.saves.Counts(ctx, ids, since)
	if err != nil {
		return nil, fmt.Errorf("count saves: %w", err)
	}
	out := make(map[uuid.UUID]ListingStats, len(ids))
	for _, id := range ids {
		v, s := views[id], saves[id]
		out[id] = ListingStats{Views: v.All, ViewsSince: v.Since, Saves: s.All, SavesSince: s.Since}
	}
	return out, nil
}

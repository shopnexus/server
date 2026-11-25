package service

import (
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"

	"golang.org/x/sync/errgroup"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoverydomain "github.com/shopnexus/server/internal/discovery/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// MyListingsScreen is the MyListingsScreen schema in
// api/schemas/screen_my_listings.yaml.
type MyListingsScreen struct {
	Week       ListingWeek      `json:"week"`
	Items      []MyListingsItem `json:"items"`
	NextCursor *uuid.UUID       `json:"next_cursor,omitempty"`
}

// ListingWeek is the ListingWeek schema: the last seven days of the
// seller's listings on sale.
type ListingWeek struct {
	Views  int `json:"views"`
	Askers int `json:"askers"`
	Saves  int `json:"saves"`
}

// MyListingsItem is the MyListingsItem schema: one of the seller's listings
// with how it does.
type MyListingsItem struct {
	Listing catalogsvc.ListingView    `json:"listing"`
	Views   int                       `json:"views"`
	Views7d int                       `json:"views_7d"`
	Askers  int                       `json:"askers"`
	Saves   int                       `json:"saves"`
	Order   *dealsvc.ListingOrderView `json:"order,omitempty"`
}

// MyListings reads a page of one of the seller's panes and, unless the page
// holds them all, the IDs of the seller's listings on sale; then, for all
// those listings at once, their views and saves, their deals, and the
// messages about them. That is four or five calls, whatever the page holds.
// The week sums the listings on sale, whichever pane the page is.
func (s *Screen) MyListings(ctx context.Context, seller uuid.UUID, pane catalogsvc.Pane, p page.Request, t locale.Tag) (MyListingsScreen, error) {
	// Views are stored by UTC day, so the week is the seven UTC days that
	// end today, for the views, the saves and the askers alike.
	since := discoverydomain.ViewWeek(s.d.Now())
	listings, err := s.d.Catalog.ListMine(ctx, seller, pane, p, t)
	if err != nil {
		return MyListingsScreen{}, fmt.Errorf("read the listings: %w", err)
	}
	shown := make([]uuid.UUID, 0, len(listings.Items))
	for i := range listings.Items {
		shown = append(shown, listings.Items[i].ID)
	}
	// The first page of the on-sale pane, with no page after it, holds
	// every listing on sale.
	onSale := shown
	if pane != catalogsvc.PaneOnSale || p.Cursor != (uuid.UUID{}) || listings.NextCursor != nil {
		if onSale, err = s.d.Catalog.OnSale(ctx, seller); err != nil {
			return MyListingsScreen{}, fmt.Errorf("read the listings on sale: %w", err)
		}
	}
	ids := make([]uuid.UUID, 0, len(onSale)+len(shown))
	seen := make(map[uuid.UUID]bool, len(onSale)+len(shown))
	for _, id := range slices.Concat(onSale, shown) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var (
		stats    map[uuid.UUID]discoverysvc.ListingStats
		activity map[uuid.UUID]dealsvc.ListingActivity
		wrote    map[uuid.UUID][]chatsvc.ListingAsker
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var statsErr error
		if stats, statsErr = s.d.Discovery.ListingStats(gctx, ids, since); statsErr != nil {
			return fmt.Errorf("read the views and saves: %w", statsErr)
		}
		return nil
	})
	g.Go(func() error {
		var dealsErr error
		if activity, dealsErr = s.d.Deals.ListingActivity(gctx, seller, ids); dealsErr != nil {
			return fmt.Errorf("read the deals: %w", dealsErr)
		}
		return nil
	})
	g.Go(func() error {
		var chatErr error
		if wrote, chatErr = s.d.Chats.ListingAskers(gctx, seller, ids); chatErr != nil {
			return fmt.Errorf("read who wrote: %w", chatErr)
		}
		return nil
	})
	if err = g.Wait(); err != nil {
		return MyListingsScreen{}, err
	}
	// A listing's askers are the people with a deal on it or who wrote to
	// the seller about it, each since the earlier of the two.
	askers := map[uuid.UUID]map[uuid.UUID]time.Time{}
	asked := func(listing, person uuid.UUID, at time.Time) {
		if askers[listing] == nil {
			askers[listing] = map[uuid.UUID]time.Time{}
		}
		if first, ok := askers[listing][person]; !ok || at.Before(first) {
			askers[listing][person] = at
		}
	}
	for id, a := range activity {
		for _, b := range a.Buyers {
			asked(id, b.ID, b.Since)
		}
	}
	for id, people := range wrote {
		for _, w := range people {
			asked(id, w.ID, w.Since)
		}
	}
	out := MyListingsScreen{Items: make([]MyListingsItem, 0, len(listings.Items)), NextCursor: listings.NextCursor}
	for _, id := range onSale {
		out.Week.Views += stats[id].ViewsSince
		out.Week.Saves += stats[id].SavesSince
		for _, at := range askers[id] {
			if !at.Before(since) {
				out.Week.Askers++
			}
		}
	}
	for i := range listings.Items {
		l := listings.Items[i]
		st := stats[l.ID]
		out.Items = append(out.Items, MyListingsItem{
			Listing: l, Views: st.Views, Views7d: st.ViewsSince, Askers: len(askers[l.ID]), Saves: st.Saves,
			Order: activity[l.ID].Order,
		})
	}
	return out, nil
}

package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// ForYou is Home's Dành cho bạn feed and its Vì bạn đã xem gần đây rail.
type ForYou struct {
	Items []SearchItem
	// Viewed is nil signed out, without a listing opened in the window, or
	// when its subcategory has no listing to show.
	Viewed *ViewedRail
}

// ViewedRail is Vì bạn đã xem gần đây: listings of the subcategory
// Category.
type ViewedRail struct {
	Category string
	Items    []SearchItem
}

// ForYou reads the first page of Dành cho bạn, page.DefaultLimit cards like
// the nearby feed's, and the Vì bạn đã xem gần đây rail. The signals are
// the viewer's newest domain.MaxSaveSignals saves and
// domain.MaxViewSignals marks since domain.ForYouSince, and the entries of
// their search history, at most domain.MaxHistory, searched since then.
// The feed is every active listing nationwide in
// catalogdomain.SortPointsNewest order under domain.ForYouBoosts of those
// signals; with no viewer or no signal it is the newest listings. The rail
// is the newest listings of domain.ViewedCategory outside the marks read.
// Both leave out the viewer's own listings and the ones search leaves out,
// and measure each card from near when it names a position.
func (d *Discovery) ForYou(ctx context.Context, viewer *uuid.UUID, near catalogdomain.Near) (ForYou, error) {
	origin, originErr := d.catalog.Origin(ctx, near)
	if originErr != nil {
		return ForYou{}, fmt.Errorf("read the position: %w", originErr)
	}
	feed := catalogdomain.Search{
		Filters: catalogdomain.Filters{Origin: origin}, Sort: catalogdomain.SortPointsNewest, Limit: page.DefaultLimit,
	}
	var rail catalogdomain.Search
	if viewer != nil {
		hidden, sellers, err := d.exclusions(ctx, *viewer)
		if err != nil {
			return ForYou{}, err
		}
		sellers = append(sellers, *viewer)
		since := domain.ForYouSince(d.now())
		marks, err := d.views.Recent(ctx, *viewer, since, domain.MaxViewSignals)
		if err != nil {
			return ForYou{}, fmt.Errorf("read views: %w", err)
		}
		saves, err := d.saves.Recent(ctx, *viewer, since, domain.MaxSaveSignals)
		if err != nil {
			return ForYou{}, fmt.Errorf("read saves: %w", err)
		}
		history, err := d.history.List(ctx, *viewer)
		if err != nil {
			return ForYou{}, fmt.Errorf("read the search history: %w", err)
		}
		opened := make([]uuid.UUID, len(marks))
		for i, m := range marks {
			opened[i] = m.Listing
		}
		listings, err := d.catalog.Summaries(ctx, slices.Concat(opened, saves))
		if err != nil {
			return ForYou{}, fmt.Errorf("read the listings of the signals: %w", err)
		}
		savedIn := make([]string, len(saves))
		for i, id := range saves {
			savedIn[i] = listings[id].CategoryCode
		}
		var searchedIn []string
		for _, e := range history {
			if !e.SearchedAt.Before(since) {
				searchedIn = append(searchedIn, e.CategoryCode)
			}
		}
		openedIn := make([]string, len(marks))
		for i := range marks {
			marks[i].Category = listings[marks[i].Listing].CategoryCode
			openedIn[i] = marks[i].Category
		}
		feed.Boosts = domain.ForYouBoosts(savedIn, searchedIn, openedIn)
		feed.ExcludeListings, feed.ExcludeSellers = hidden, sellers
		if category, ok := domain.ViewedCategory(marks); ok {
			rail = catalogdomain.Search{
				Filters: catalogdomain.Filters{CategoryCode: category, Origin: origin}, Sort: catalogdomain.SortNewest,
				Limit: domain.MaxViewed, ExcludeListings: slices.Concat(hidden, opened), ExcludeSellers: sellers,
			}
		}
	}

	cards, err := d.catalog.Search(ctx, feed)
	if err != nil {
		return ForYou{}, fmt.Errorf("read the feed: %w", err)
	}
	cards = domain.Distinct(cards, cardKey)
	var out ForYou
	if out.Items, err = d.withSaved(ctx, viewer, cards); err != nil {
		return ForYou{}, err
	}
	if rail.Filters.CategoryCode == "" {
		return out, nil
	}
	if cards, err = d.catalog.Search(ctx, rail); err != nil {
		return ForYou{}, fmt.Errorf("read the viewed rail: %w", err)
	}
	cards = domain.Distinct(cards, cardKey)
	if len(cards) == 0 {
		return out, nil
	}
	items, err := d.withSaved(ctx, viewer, cards)
	if err != nil {
		return ForYou{}, err
	}
	out.Viewed = &ViewedRail{Category: rail.Filters.CategoryCode, Items: items}
	return out, nil
}

// cardKey is what makes two cards the same listing posted twice.
func cardKey(c catalogsvc.SearchCard) (uuid.UUID, string) { return c.SellerID, c.Title }

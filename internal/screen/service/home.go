package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/locale"
)

// HomeCategory is a root of the category tree with its subcategories.
type HomeCategory struct {
	Code        string            `json:"code"`
	DisplayName string            `json:"display_name"`
	Children    []HomeSubcategory `json:"children"`
}

// HomeSubcategory is a subcategory under a root.
type HomeSubcategory struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
}

// HomeScreen is the HomeScreen schema in api/schemas/screen_home.yaml.
type HomeScreen struct {
	Categories []HomeCategory          `json:"categories"`
	Nearby     discoverysvc.SearchView `json:"nearby"`
	// ForYou is Dành cho bạn and Viewed is Vì bạn đã xem gần đây, nil when
	// discovery has no rail for the viewer.
	ForYou []discoverysvc.SearchItem `json:"for_you"`
	Viewed *HomeViewed               `json:"viewed,omitempty"`
	// NotificationsUnread is the viewer's unread notices, 0 signed out.
	NotificationsUnread int `json:"notifications_unread"`
}

// HomeViewed is the HomeViewed schema: listings of one subcategory.
type HomeViewed struct {
	Category catalogdomain.Category    `json:"category"`
	Items    []discoverysvc.SearchItem `json:"items"`
}

// Home reads the category tree, the active listings under each root and in
// each subcategory, the first page of the search sorted SortNearby around
// near within radiusKm (the search's own radius when 0), and discovery's
// ForYou measured from near. Without a position the nearby feed is empty;
// radiusKm without one is the search's
// discoverydomain.ErrPositionRequired. Categories are named in t.
func (s *Screen) Home(ctx context.Context, viewer *uuid.UUID, near catalogdomain.Near, radiusKm int, t locale.Tag) (HomeScreen, error) {
	out := HomeScreen{Nearby: discoverysvc.SearchView{Items: []discoverysvc.SearchItem{}}}
	if near != (catalogdomain.Near{}) || radiusKm > 0 {
		nearby, err := s.d.Discovery.Search(ctx, viewer, discoverysvc.SearchInput{Near: near, RadiusKm: radiusKm, Sort: discoverysvc.SortNearby, Locale: t})
		if err != nil {
			return HomeScreen{}, fmt.Errorf("read the nearby feed: %w", err)
		}
		out.Nearby = nearby
	}
	forYou, err := s.d.Discovery.ForYou(ctx, viewer, near)
	if err != nil {
		return HomeScreen{}, fmt.Errorf("read the for-you feed: %w", err)
	}
	out.ForYou = forYou.Items
	tree, err := s.d.Catalog.Categories(ctx, t)
	if err != nil {
		return HomeScreen{}, fmt.Errorf("read categories: %w", err)
	}
	if v := forYou.Viewed; v != nil {
		for _, c := range tree {
			if c.Code == v.Category {
				out.Viewed = &HomeViewed{Category: c, Items: v.Items}
				break
			}
		}
	}
	if viewer != nil {
		if out.NotificationsUnread, err = s.d.Notifications.Unread(ctx, *viewer); err != nil {
			return HomeScreen{}, fmt.Errorf("count unread notices: %w", err)
		}
	}
	index := map[string]int{}
	for _, c := range tree {
		if c.ParentCode == "" {
			index[c.Code] = len(out.Categories)
			out.Categories = append(out.Categories, HomeCategory{Code: c.Code, DisplayName: c.DisplayName, Children: []HomeSubcategory{}})
			continue
		}
		if i, ok := index[c.ParentCode]; ok {
			out.Categories[i].Children = append(out.Categories[i].Children, HomeSubcategory{Code: c.Code, DisplayName: c.DisplayName})
		}
	}
	return out, nil
}

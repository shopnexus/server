package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
)

// HomeCategory is a root of the category tree with its active listings
// counted and its subcategories.
type HomeCategory struct {
	Code        string                   `json:"code"`
	DisplayName string                   `json:"display_name"`
	Count       int                      `json:"count"`
	Children    []catalogdomain.Category `json:"children"`
}

// HomeScreen is the HomeScreen schema in api/schemas/screen_home.yaml.
type HomeScreen struct {
	Categories []HomeCategory          `json:"categories"`
	Nearby     discoverysvc.SearchView `json:"nearby"`
	// NotificationsUnread is the reader's unread notices, 0 signed out.
	NotificationsUnread int `json:"notifications_unread"`
}

// Home reads the category tree, the active listings under each root, and
// the first page of the nearby feed around near.
func (s *Screen) Home(ctx context.Context, reader *uuid.UUID, near discoverysvc.Near) (HomeScreen, error) {
	nearby, err := s.saves.Nearby(ctx, reader, near, "", 0)
	if err != nil {
		return HomeScreen{}, fmt.Errorf("read the nearby feed: %w", err)
	}
	tree, err := s.catalog.Categories(ctx)
	if err != nil {
		return HomeScreen{}, fmt.Errorf("read categories: %w", err)
	}
	counts, err := s.catalog.Counts(ctx, catalogdomain.Search{}, catalogdomain.CountRoot)
	if err != nil {
		return HomeScreen{}, fmt.Errorf("count listings: %w", err)
	}
	byRoot := make(map[string]int, len(counts))
	for _, c := range counts {
		byRoot[c.Code] = c.Count
	}
	out := HomeScreen{Nearby: nearby}
	if reader != nil {
		if out.NotificationsUnread, err = s.notices.Unread(ctx, *reader); err != nil {
			return HomeScreen{}, fmt.Errorf("count unread notices: %w", err)
		}
	}
	index := map[string]int{}
	for _, c := range tree {
		if c.ParentCode == "" {
			index[c.Code] = len(out.Categories)
			out.Categories = append(out.Categories, HomeCategory{Code: c.Code, DisplayName: c.DisplayName, Count: byRoot[c.Code]})
			continue
		}
		if i, ok := index[c.ParentCode]; ok {
			out.Categories[i].Children = append(out.Categories[i].Children, c)
		}
	}
	return out, nil
}

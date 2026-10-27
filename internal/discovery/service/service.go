// Package service holds module discovery's use cases: a person saves
// listings and reads them back, searches, and reads the nearby feed.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Service is module discovery's contract.
type Service interface {
	// Save keeps someone else's listing on sale or being shipped, with its
	// price; saving it again changes nothing.
	Save(ctx context.Context, account, listing uuid.UUID) error
	Unsave(ctx context.Context, account, listing uuid.UUID) error
	// IsSaved reports whether account saved listing.
	IsSaved(ctx context.Context, account, listing uuid.UUID) (bool, error)
	// Saved lists the account's saves, newest first.
	Saved(ctx context.Context, account uuid.UUID, p page.Request) (page.Page[SavedView], error)
	// Search finds active listings; viewer, nil when signed out, sees which
	// they saved.
	Search(ctx context.Context, viewer *uuid.UUID, in SearchInput) (SearchView, error)
	// Nearby is the nearby feed around near.
	Nearby(ctx context.Context, viewer *uuid.UUID, near Near, cursor string, limit int) (SearchView, error)
}

// Deps is what New builds Discovery from.
type Deps struct {
	Saves          repository.Saves
	Understandings repository.Understandings
	Catalog        catalogsvc.Service
	LLM            llm.Client
	// ModelTimeout is domain.ModelTimeout in production; tests shorten it.
	ModelTimeout time.Duration
	Log          *slog.Logger
	Now          func() time.Time
}

// Discovery implements Service in this process.
type Discovery struct {
	saves          repository.Saves
	understandings repository.Understandings
	catalog        catalogsvc.Service
	llm            llm.Client
	modelTimeout   time.Duration
	log            *slog.Logger
	now            func() time.Time
}

var _ Service = (*Discovery)(nil)

func New(d Deps) *Discovery {
	return &Discovery{
		saves: d.Saves, understandings: d.Understandings, catalog: d.Catalog, llm: d.LLM,
		modelTimeout: d.ModelTimeout, log: d.Log, now: d.Now,
	}
}

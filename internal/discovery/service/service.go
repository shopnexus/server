// Package service holds module discovery's use cases: for now a person
// saves listings and reads them back.
package service

import (
	"context"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/page"
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
}

// Deps is what New builds Discovery from.
type Deps struct {
	Saves   repository.Saves
	Catalog catalogsvc.Service
	Now     func() time.Time
}

// Discovery implements Service in this process.
type Discovery struct {
	saves   repository.Saves
	catalog catalogsvc.Service
	now     func() time.Time
}

var _ Service = (*Discovery)(nil)

func New(d Deps) *Discovery { return &Discovery{saves: d.Saves, catalog: d.Catalog, now: d.Now} }

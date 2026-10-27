// Package service holds module trust's use cases: for now a buyer reviews
// the seller of a shipped order, and anyone reads a seller's profile and
// reviews.
package service

import (
	"context"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/repository"
)

// Service is module trust's contract.
type Service interface {
	// PutReview creates the buyer's review of an order, or edits it within
	// its 7 days.
	PutReview(ctx context.Context, buyer, order uuid.UUID, stars int, text string) (ReviewView, error)
	// DeleteReview removes the buyer's review of an order within its 7 days.
	DeleteReview(ctx context.Context, buyer, order uuid.UUID) error
	// ReviewFacts reads a review for its notification; false for an
	// unknown one.
	ReviewFacts(ctx context.Context, id uuid.UUID) (ReviewFacts, bool, error)
	// SellerReviews reads a seller's average, count and reviews, newest first.
	SellerReviews(ctx context.Context, seller uuid.UUID, p page.Request) (SellerReviews, error)
	// Seller reads a seller's public profile with the rating and the
	// listing counts.
	Seller(ctx context.Context, id uuid.UUID) (SellerView, error)
}

// Deps is what New builds Trust from.
type Deps struct {
	Reviews  repository.Reviews
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Now      func() time.Time
}

// Trust implements Service in this process.
type Trust struct {
	reviews  repository.Reviews
	accounts accountsvc.Service
	catalog  catalogsvc.Service
	deals    dealsvc.Service
	now      func() time.Time
}

var _ Service = (*Trust)(nil)

func New(d Deps) *Trust {
	return &Trust{reviews: d.Reviews, accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, now: d.Now}
}

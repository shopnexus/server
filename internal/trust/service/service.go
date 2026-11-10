// Package service holds module trust's use cases: a buyer reviews the
// seller of a shipped order, anyone reads a seller's rating and reviews,
// and people report listings and accounts.
package service

import (
	"context"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
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
	// SellerReviews reads a page of a seller's reviews, newest first: the
	// ReviewList schema in api/schemas/trust.yaml.
	SellerReviews(ctx context.Context, seller uuid.UUID, p page.Request) (page.Page[ReviewView], error)
	// Rating reads the average and count over a seller's reviews.
	Rating(ctx context.Context, seller uuid.UUID) (Rating, error)
	// Report opens the reporter's report of a listing or an account, or
	// replaces the reason, note and conversation of their open report of it;
	// the bool is whether it opened one.
	Report(ctx context.Context, reporter uuid.UUID, in ReportInput) (ReportView, bool, error)
	// WithdrawReport withdraws one of the reporter's reports; a withdrawn
	// one is fine.
	WithdrawReport(ctx context.Context, reporter, id uuid.UUID) error
}

// Deps is what New builds Trust from.
type Deps struct {
	Reviews  repository.Reviews
	Reports  repository.Reports
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	// Chats tells whether a report's conversation is the reporter's with
	// the account.
	Chats chatsvc.Service
	Now   func() time.Time
}

// Trust implements Service in this process.
type Trust struct {
	reviews  repository.Reviews
	reports  repository.Reports
	accounts accountsvc.Service
	catalog  catalogsvc.Service
	deals    dealsvc.Service
	chats    chatsvc.Service
	now      func() time.Time
}

var _ Service = (*Trust)(nil)

func New(d Deps) *Trust {
	return &Trust{reviews: d.Reviews, reports: d.Reports, accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, chats: d.Chats, now: d.Now}
}

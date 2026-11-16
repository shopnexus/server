// Package service holds module trust's use cases: a buyer reviews the
// seller of a shipped order, anyone reads a seller's rating and reviews,
// people report listings and accounts, and members of a conversation are
// warned of scams.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/repository"
)

// Service is module trust's contract.
type Service interface {
	OwnReview(ctx context.Context, buyer, order uuid.UUID) (OwnReview, error)
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
	// ScamWarnings reads account's scam warnings in the conversations
	// given, keyed by conversation; one without a warning is left out.
	ScamWarnings(ctx context.Context, account uuid.UUID, conversations []uuid.UUID) (map[uuid.UUID]ScamWarningView, error)
	// DismissScamWarning dismisses account's warning in a conversation of
	// account's, and answers chat's conversation_not_found to anyone but a
	// member. Without a warning, or dismissed already, it changes nothing.
	DismissScamWarning(ctx context.Context, account, conversation uuid.UUID) error
	// DeleteScamWarnings deletes a deleted account's own warnings.
	DeleteScamWarnings(ctx context.Context, account uuid.UUID) error
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
	Scams repository.ScamWarnings
	// Classifier reads a check's window; Facts add fact signals beside a
	// message code.
	Classifier Classifier
	Facts      []FactSource
	Log        *slog.Logger
	// Prefix names this process's Restate services.
	Prefix string
	Now    func() time.Time
}

// Trust implements Service in this process.
type Trust struct {
	reviews    repository.Reviews
	reports    repository.Reports
	accounts   accountsvc.Service
	catalog    catalogsvc.Service
	deals      dealsvc.Service
	chats      chatsvc.Service
	scams      repository.ScamWarnings
	classifier Classifier
	facts      []FactSource
	log        *slog.Logger
	prefix     string
	now        func() time.Time
}

var _ Service = (*Trust)(nil)

func New(d Deps) *Trust {
	return &Trust{
		reviews: d.Reviews, reports: d.Reports, accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, chats: d.Chats,
		scams: d.Scams, classifier: d.Classifier, facts: d.Facts, log: d.Log, prefix: d.Prefix, now: d.Now,
	}
}

// Durable returns module trust's Restate services, for Mount to bind.
func (t *Trust) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(ScamCheck{
		Prefix: t.prefix, Chats: t.chats, Scams: t.scams, Classifier: t.classifier, Facts: t.facts, Log: t.log, Now: t.now,
	})}
}

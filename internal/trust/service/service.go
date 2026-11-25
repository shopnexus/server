// Package service holds module trust's use cases: the two sides of a closed
// order review each other and the subject replies once, anyone reads a
// person's ratings and reviews,
// people report listings and accounts, console members work the report
// cases, and members of a conversation are warned of scams.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// Service is module trust's contract.
type Service interface {
	// OrderReviews reads the caller's review and the other side's review of
	// each order given, keyed by order; the caller is a party to each.
	OrderReviews(ctx context.Context, caller uuid.UUID, orders []uuid.UUID) (map[uuid.UUID]OrderReviews, error)
	// PutReview creates the caller's review of the other party to an
	// order, or edits it within its 7 days and before a reply. photos are
	// upload IDs in order; nil tags or photos keep the review's.
	PutReview(ctx context.Context, caller, order uuid.UUID, stars int, text string, tags []domain.Tag, photos []uuid.UUID) (ReviewView, error)
	// DeleteReview removes the caller's review of an order within its 7
	// days and before a reply.
	DeleteReview(ctx context.Context, caller, order uuid.UUID) error
	// ReplyReview is the subject's one reply to a visible review.
	ReplyReview(ctx context.Context, caller, review uuid.UUID, text string) (ReviewView, error)
	// ReviewFacts reads a review for its notifications; false for an
	// unknown or deleted one.
	ReviewFacts(ctx context.Context, id uuid.UUID) (ReviewFacts, bool, error)
	// Reviews reads a page of a person's visible reviews in one role,
	// newest first, as viewer reads them (nil signed out): the ReviewList
	// schema in api/schemas/trust.yaml.
	Reviews(ctx context.Context, viewer *uuid.UUID, subject uuid.UUID, role domain.Role, p page.Request) (page.Page[ReviewView], error)
	// MarkHelpful records that the caller found a visible review helpful;
	// its reviewer and subject cannot.
	MarkHelpful(ctx context.Context, caller, review uuid.UUID) error
	// UnmarkHelpful takes the caller's helpful mark back.
	UnmarkHelpful(ctx context.Context, caller, review uuid.UUID) error
	// Rating reads the average, count and tag counts over a person's
	// visible reviews in one role.
	Rating(ctx context.Context, subject uuid.UUID, role domain.Role) (Rating, error)
	// Report opens the reporter's report of a listing or an account, or
	// replaces the reason, note and conversation of their open report of it;
	// the bool is whether it opened one.
	Report(ctx context.Context, reporter uuid.UUID, in ReportInput) (ReportView, bool, error)
	// MyReports lists the reporter's reports, newest first, withdrawn and
	// resolved ones included.
	MyReports(ctx context.Context, reporter uuid.UUID, p page.Request) (page.Page[ReportView], error)
	// WithdrawReport withdraws one of the reporter's reports; a withdrawn
	// one is fine.
	WithdrawReport(ctx context.Context, reporter, id uuid.UUID) error
	// ReportCases lists the open report cases, latest report first and with
	// their count, or the resolved ones, latest resolution first, for
	// console members.
	ReportCases(ctx context.Context, status domain.CaseStatus, p page.Request) (CaseList, error)
	// ReportCase reads account's open report case or, without one, its
	// latest resolved case.
	ReportCase(ctx context.Context, account uuid.UUID) (CaseDetail, error)
	// CountReportCases counts the open cases with a report made at or after
	// since and the cases resolved at or after since, the zero time for all.
	CountReportCases(ctx context.Context, since time.Time) (map[domain.CaseStatus]int, error)
	// ResolveReportCase closes account's open report case with outcome for
	// the console member by.
	ResolveReportCase(ctx context.Context, account uuid.UUID, outcome domain.Outcome, by audit.Actor) (CaseDetail, error)
	// ReopenReportCase undoes account's latest resolution for the console
	// member by; its reports join the open case.
	ReopenReportCase(ctx context.Context, account uuid.UUID, by audit.Actor) (CaseDetail, error)
	// ScamWarnings reads account's scam warnings in the conversations
	// given, keyed by conversation, with the signals' text in t; one
	// without a warning is left out.
	ScamWarnings(ctx context.Context, account uuid.UUID, conversations []uuid.UUID, t locale.Tag) (map[uuid.UUID]ScamWarningView, error)
	// DismissScamWarning dismisses account's warning in a conversation of
	// account's, and answers chat's conversation_not_found to anyone but a
	// member. Without a warning, or dismissed already, it changes nothing.
	DismissScamWarning(ctx context.Context, account, conversation uuid.UUID) error
	// ClearAccount deletes a deleted account's own scam warnings and helpful
	// marks, and withdraws its unresolved reports with their notes blanked.
	ClearAccount(ctx context.Context, account uuid.UUID) error
}

// Deps is what New builds Trust from.
type Deps struct {
	Reviews  repository.Reviews
	Reports  repository.Reports
	Cases    repository.Cases
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	// Media keeps and processes reviews' photos.
	Media mediasvc.Service
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
	cases      repository.Cases
	accounts   accountsvc.Service
	catalog    catalogsvc.Service
	deals      dealsvc.Service
	media      mediasvc.Service
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
		reviews: d.Reviews, reports: d.Reports, cases: d.Cases, accounts: d.Accounts, catalog: d.Catalog, deals: d.Deals, media: d.Media, chats: d.Chats,
		scams: d.Scams, classifier: d.Classifier, facts: d.Facts, log: d.Log, prefix: d.Prefix, now: d.Now,
	}
}

// Durable returns module trust's Restate services, for Mount to bind.
func (t *Trust) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(ScamCheck{
		Prefix: t.prefix, Chats: t.chats, Scams: t.scams, Classifier: t.classifier, Facts: t.facts, Log: t.log, Now: t.now,
	})}
}

// ClearAccount clears a deleted account's trust rows; each write is
// idempotent, so a retried run changes nothing more.
func (t *Trust) ClearAccount(ctx context.Context, account uuid.UUID) error {
	if err := t.scams.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete scam warnings: %w", err)
	}
	if err := t.reports.WithdrawAll(ctx, account, t.now()); err != nil {
		return fmt.Errorf("withdraw reports: %w", err)
	}
	if err := t.reviews.UnmarkAll(ctx, account); err != nil {
		return fmt.Errorf("unmark helpful: %w", err)
	}
	return nil
}

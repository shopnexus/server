package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// ReportCases lists one page of cases: the open ones by their latest
// report, with their count, or the resolved ones by their resolution, each
// newest first. Each case shows its account and its most reported listing,
// read in one query each. It is UC-MOD-02 step 1.
func (s *Trust) ReportCases(ctx context.Context, status domain.CaseStatus, p page.Request) (CaseList, error) {
	var (
		out CaseList
		cs  []repository.Case
		err error
	)
	if status == domain.CaseResolved {
		cs, err = s.d.Cases.Resolved(ctx, p)
	} else {
		cs, err = s.d.Cases.Open(ctx, p)
		if err == nil {
			var n int
			n, err = s.d.Cases.CountOpen(ctx)
			out.Total = &n
		}
	}
	if err != nil {
		return CaseList{}, fmt.Errorf("read the cases: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(cs))
	top := make([]uuid.UUID, len(cs))
	var listings []uuid.UUID
	for i := range cs {
		ids = append(ids, cs[i].AccountID)
		if ranked := domain.RankListings(cs[i].Listings); len(ranked) > 0 {
			top[i] = ranked[0]
			listings = append(listings, ranked[0])
		}
	}
	accounts, err := s.d.Accounts.ConsoleAccountsOf(ctx, ids)
	if err != nil {
		return CaseList{}, fmt.Errorf("read the accounts: %w", err)
	}
	shown, err := s.d.Catalog.ReportedListings(ctx, listings)
	if err != nil {
		return CaseList{}, fmt.Errorf("read the listings: %w", err)
	}
	items := make([]ReportCase, 0, len(cs))
	for i := range cs {
		c := ReportCase{
			Account: accounts[cs[i].AccountID], Reports: cs[i].Reports, Reasons: cs[i].Reasons, FirstAt: cs[i].FirstAt,
			LastAt: cs[i].LastAt, Resolution: resolutionOf(cs[i].Resolution), key: cs[i].Key,
		}
		if l, ok := shown[top[i]]; ok {
			c.Listing = &l
		}
		items = append(items, c)
	}
	out.Page = page.Of(items, p, func(c ReportCase) uuid.UUID { return c.key })
	return out, nil
}

// ReportCase reads account's open case or, without one, its latest
// resolved case; an account nobody has reported, withdrawn reports aside,
// is domain.ErrCaseNotFound. It is UC-MOD-02 step 2.
func (s *Trust) ReportCase(ctx context.Context, account uuid.UUID) (CaseDetail, error) {
	// E1
	c, rs, err := s.d.Cases.Current(ctx, account)
	if errors.Is(err, repository.ErrNotFound) {
		return CaseDetail{}, domain.ErrCaseNotFound
	}
	if err != nil {
		return CaseDetail{}, fmt.Errorf("read the case: %w", err)
	}
	a, err := s.d.Accounts.ConsoleAccount(ctx, account)
	if err != nil {
		return CaseDetail{}, fmt.Errorf("read the account: %w", err)
	}
	reporters := make([]uuid.UUID, 0, len(rs))
	for i := range rs {
		reporters = append(reporters, rs[i].ReporterID())
	}
	people, err := s.d.Accounts.GetMany(ctx, reporters)
	if err != nil {
		return CaseDetail{}, fmt.Errorf("read the reporters: %w", err)
	}
	ranked := domain.RankListings(c.Listings)
	shown, err := s.d.Catalog.ReportedListings(ctx, ranked)
	if err != nil {
		return CaseDetail{}, fmt.Errorf("read the listings: %w", err)
	}
	out := CaseDetail{
		Account: a, Reasons: c.Reasons, FirstAt: c.FirstAt, LastAt: c.LastAt, Resolution: resolutionOf(c.Resolution),
		Listings: make([]catalogsvc.ReportedListing, 0, len(ranked)), Reports: make([]CaseReport, 0, len(rs)),
	}
	for _, id := range ranked {
		if l, ok := shown[id]; ok {
			out.Listings = append(out.Listings, l)
		}
	}
	for i := range rs {
		r := &rs[i]
		out.Reports = append(out.Reports, CaseReport{
			ID: r.ID(), Reporter: Reporter{ID: r.ReporterID(), DisplayName: people[r.ReporterID()].DisplayName}, Reason: r.Reason(),
			Note: r.Note(), ListingID: r.ListingID(), ConversationID: r.ConversationID(), CreatedAt: r.CreatedAt(),
		})
	}
	return out, nil
}

// ResolveReportCase closes account's open case with outcome for the member
// by, recording report.dismiss or report.resolve in the same transaction.
// Without an open case it answers the latest resolved one when that has
// outcome (a retry), else domain.ErrCaseNotOpen; the ShopNexus account is
// accountdomain.ErrSystemAccount. It is UC-MOD-02 step 4 and A1.
func (s *Trust) ResolveReportCase(ctx context.Context, account uuid.UUID, outcome domain.Outcome, by audit.Actor) (CaseDetail, error) {
	// E6
	if account == accountdomain.ShopNexusID {
		return CaseDetail{}, accountdomain.ErrSystemAccount
	}
	now := s.d.Now()
	action := audit.ReportResolve
	if outcome == domain.OutcomeDismissed {
		action = audit.ReportDismiss
	}
	// 4. A1 closes the case as no violation. E2 without an open case,
	// unless the latest resolution has this outcome (A4, a retry).
	id := uuid.NewV7()
	err := s.d.Cases.Resolve(ctx, account, repository.Resolution{ID: id, Outcome: outcome, MemberID: by.MemberID, At: now}, audit.Entry{
		Actor: by, Action: action, Target: audit.Account(account), Details: map[string]any{"resolution_id": id}, At: now,
	})
	if err != nil {
		return CaseDetail{}, fmt.Errorf("resolve the case: %w", err)
	}
	return s.ReportCase(ctx, account)
}

// ReopenReportCase undoes account's latest resolution for the member by:
// its reports join the account's open case, and report.reopen is recorded
// in the same transaction. Without a resolution it is
// domain.ErrCaseNotResolved; the ShopNexus account is
// accountdomain.ErrSystemAccount. It is UC-MOD-02 A2.
func (s *Trust) ReopenReportCase(ctx context.Context, account uuid.UUID, by audit.Actor) (CaseDetail, error) {
	// E6
	if account == accountdomain.ShopNexusID {
		return CaseDetail{}, accountdomain.ErrSystemAccount
	}
	// A2. E3 without a resolved case.
	now := s.d.Now()
	err := s.d.Cases.Reopen(ctx, account, now, audit.Entry{Actor: by, Action: audit.ReportReopen, Target: audit.Account(account), At: now})
	if errors.Is(err, repository.ErrConflict) {
		return CaseDetail{}, apperr.ErrTryAgain
	}
	if err != nil {
		return CaseDetail{}, fmt.Errorf("reopen the case: %w", err)
	}
	return s.ReportCase(ctx, account)
}

func (s *Trust) CountReportCases(ctx context.Context, since time.Time) (map[domain.CaseStatus]int, error) {
	n, err := s.d.Cases.CountByStatus(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("count the cases: %w", err)
	}
	return n, nil
}

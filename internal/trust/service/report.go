package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// Report checks that the reporter may write and names one target that
// exists, takes a listing's seller as the account reported, checks that a
// named conversation is the pair's, then opens the report or updates the
// open one.
func (s *Trust) Report(ctx context.Context, reporter uuid.UUID, in ReportInput) (ReportView, bool, error) {
	status, err := s.d.Accounts.Status(ctx, reporter)
	if err != nil {
		return ReportView{}, false, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReportView{}, false, r
	}
	if (in.ListingID == uuid.UUID{}) == (in.AccountID == uuid.UUID{}) {
		return ReportView{}, false, domain.ErrInvalidReport.WithField("target", apperr.OneOf, "exactly one of listing_id and account_id")
	}
	if in.AccountID == accountdomain.ShopNexusID {
		return ReportView{}, false, accountdomain.ErrSystemAccount
	}
	account := in.AccountID
	if in.ListingID != (uuid.UUID{}) {
		l, found, lerr := s.d.Catalog.Summary(ctx, in.ListingID)
		if lerr != nil {
			return ReportView{}, false, fmt.Errorf("read listing: %w", lerr)
		}
		if !found || l.Status == catalogdomain.StatusDeleted {
			return ReportView{}, false, catalogdomain.ErrNotFound
		}
		account = l.SellerID
	} else if _, err = s.d.Accounts.Get(ctx, account); err != nil {
		return ReportView{}, false, fmt.Errorf("read account: %w", err)
	}
	if in.ConversationID != (uuid.UUID{}) && in.ListingID == (uuid.UUID{}) {
		pair, cerr := s.d.Chats.ConversationWith(ctx, reporter, account)
		if cerr != nil {
			return ReportView{}, false, fmt.Errorf("find the pair's conversation: %w", cerr)
		}
		if pair != in.ConversationID {
			return ReportView{}, false, domain.ErrInvalidReport.WithField("conversation_id", apperr.NotAllowed, "the reporter's conversation with the account")
		}
	}
	r, err := domain.NewReport(uuid.NewV7(), reporter, account, in.ListingID, in.ConversationID, in.Reason, in.Note, s.d.Now())
	if err != nil {
		return ReportView{}, false, err
	}
	r, added, err := s.d.Reports.Open(ctx, r)
	if err != nil {
		return ReportView{}, false, fmt.Errorf("open report: %w", err)
	}
	return reportView(r), added, nil
}

// MyReports lists a page of the reporter's reports, newest first.
func (s *Trust) MyReports(ctx context.Context, reporter uuid.UUID, p page.Request) (page.Page[ReportView], error) {
	rows, err := s.d.Reports.ListMine(ctx, reporter, p)
	if err != nil {
		return page.Page[ReportView]{}, fmt.Errorf("list reports: %w", err)
	}
	pg := page.Of(rows, p, func(r domain.Report) uuid.UUID { return r.ID() })
	out := page.Page[ReportView]{Items: make([]ReportView, 0, len(pg.Items)), NextCursor: pg.NextCursor}
	for i := range pg.Items {
		out.Items = append(out.Items, reportView(pg.Items[i]))
	}
	return out, nil
}

// WithdrawReport withdraws the reporter's report; anyone else's, or an
// unknown ID, is domain.ErrReportNotFound.
func (s *Trust) WithdrawReport(ctx context.Context, reporter, id uuid.UUID) error {
	status, err := s.d.Accounts.Status(ctx, reporter)
	if err != nil {
		return fmt.Errorf("read account status: %w", err)
	}
	if refusal := status.Refusal(); refusal != nil {
		return refusal
	}
	r, err := s.d.Reports.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.ReporterID() != reporter) {
		return domain.ErrReportNotFound
	}
	if err != nil {
		return fmt.Errorf("get report: %w", err)
	}
	if err = s.d.Reports.Withdraw(ctx, id, s.d.Now()); err != nil {
		return fmt.Errorf("withdraw report: %w", err)
	}
	return nil
}

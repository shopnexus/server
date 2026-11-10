package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
)

// Report checks that the reporter may write and names one target that
// exists, takes a listing's seller as the account reported, checks that a
// named conversation is the pair's, then opens the report or updates the
// open one.
func (s *Trust) Report(ctx context.Context, reporter uuid.UUID, in ReportInput) (ReportView, bool, error) {
	status, err := s.accounts.Status(ctx, reporter)
	if err != nil {
		return ReportView{}, false, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReportView{}, false, r
	}
	if (in.ListingID == uuid.UUID{}) == (in.AccountID == uuid.UUID{}) {
		return ReportView{}, false, domain.ErrInvalidReport.WithField("target", "exactly one of listing_id and account_id")
	}
	account := in.AccountID
	if in.ListingID != (uuid.UUID{}) {
		l, found, lerr := s.catalog.Summary(ctx, in.ListingID)
		if lerr != nil {
			return ReportView{}, false, fmt.Errorf("read listing: %w", lerr)
		}
		if !found || l.Status == catalogdomain.StatusDeleted {
			return ReportView{}, false, catalogdomain.ErrNotFound
		}
		account = l.SellerID
	} else if _, err = s.accounts.Get(ctx, account); err != nil {
		return ReportView{}, false, fmt.Errorf("read account: %w", err)
	}
	if in.ConversationID != (uuid.UUID{}) && in.ListingID == (uuid.UUID{}) {
		pair, cerr := s.chats.ConversationWith(ctx, reporter, account)
		if cerr != nil {
			return ReportView{}, false, fmt.Errorf("find the pair's conversation: %w", cerr)
		}
		if pair != in.ConversationID {
			return ReportView{}, false, domain.ErrInvalidReport.WithField("conversation_id", "the reporter's conversation with the account")
		}
	}
	r, err := domain.NewReport(uuid.NewV7(), reporter, account, in.ListingID, in.ConversationID, in.Reason, in.Note, s.now())
	if err != nil {
		return ReportView{}, false, err
	}
	r, added, err := s.reports.Open(ctx, r)
	if err != nil {
		return ReportView{}, false, fmt.Errorf("open report: %w", err)
	}
	return ReportView{
		ID: r.ID(), ListingID: r.ListingID(), AccountID: r.AccountID(), ConversationID: r.ConversationID(), Reason: r.Reason(), Note: r.Note(),
		CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
	}, added, nil
}

// WithdrawReport withdraws the reporter's report; anyone else's, or an
// unknown ID, is domain.ErrReportNotFound.
func (s *Trust) WithdrawReport(ctx context.Context, reporter, id uuid.UUID) error {
	r, err := s.reports.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.ReporterID() != reporter) {
		return domain.ErrReportNotFound
	}
	if err != nil {
		return fmt.Errorf("get report: %w", err)
	}
	if err = s.reports.Withdraw(ctx, id, s.now()); err != nil {
		return fmt.Errorf("withdraw report: %w", err)
	}
	return nil
}

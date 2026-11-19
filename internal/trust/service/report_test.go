package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

func TestReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	reporter, seller := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)

	v, added, err := e.trust.Report(ctx, reporter, service.ReportInput{ListingID: listing, Reason: domain.ReasonFake})
	if err != nil || !added || v.AccountID != seller || v.ListingID != listing || v.Reason != domain.ReasonFake {
		t.Fatalf("listing: %+v %v %v", v, added, err)
	}
	again, added, err := e.trust.Report(ctx, reporter, service.ReportInput{ListingID: listing, Reason: domain.ReasonScam, Note: "Đòi cọc"})
	if err != nil || added || again.ID != v.ID || again.Note != "Đòi cọc" {
		t.Fatalf("again: %+v %v %v", again, added, err)
	}
	person, added, err := e.trust.Report(ctx, reporter, service.ReportInput{AccountID: seller, Reason: domain.ReasonScam})
	if err != nil || !added || person.ID == v.ID || person.ListingID != (uuid.UUID{}) {
		t.Fatalf("account: %+v %v %v", person, added, err)
	}

	for name, c := range map[string]struct {
		in   service.ReportInput
		want error
	}{
		"both targets":    {service.ReportInput{ListingID: listing, AccountID: seller, Reason: domain.ReasonFake}, domain.ErrInvalidReport},
		"no target":       {service.ReportInput{Reason: domain.ReasonFake}, domain.ErrInvalidReport},
		"unknown listing": {service.ReportInput{ListingID: uuid.NewV7(), Reason: domain.ReasonFake}, catalogdomain.ErrNotFound},
		"unknown account": {service.ReportInput{AccountID: uuid.NewV7(), Reason: domain.ReasonFake}, accountdomain.ErrAccountNotFound},
		"oneself":         {service.ReportInput{AccountID: reporter, Reason: domain.ReasonFake}, domain.ErrSelfReport},
		"unknown reason":  {service.ReportInput{AccountID: seller, Reason: "ugly"}, domain.ErrInvalidReport},
	} {
		if _, _, cerr := e.trust.Report(ctx, reporter, c.in); !errors.Is(cerr, c.want) {
			t.Errorf("%s: %v", name, cerr)
		}
	}
	if _, _, err = e.trust.Report(ctx, seller, service.ReportInput{ListingID: listing, Reason: domain.ReasonFake}); !errors.Is(err, domain.ErrSelfReport) {
		t.Fatalf("own listing: %v", err)
	}

	if err = e.trust.WithdrawReport(ctx, seller, v.ID); !errors.Is(err, domain.ErrReportNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err = e.trust.WithdrawReport(ctx, reporter, uuid.NewV7()); !errors.Is(err, domain.ErrReportNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	for range 2 {
		if err = e.trust.WithdrawReport(ctx, reporter, v.ID); err != nil {
			t.Fatal(err)
		}
	}

	// The reporter's list pages newest first and keeps the withdrawn one.
	first, err := e.trust.MyReports(ctx, reporter, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != person.ID || first.Items[0].Status != domain.ReportOpen || first.NextCursor == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := e.trust.MyReports(ctx, reporter, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != v.ID || second.Items[0].Status != domain.ReportWithdrawn || second.NextCursor != nil {
		t.Fatalf("second page: %+v %v", second, err)
	}
	if theirs, listErr := e.trust.MyReports(ctx, seller, page.Request{}); listErr != nil || len(theirs.Items) != 0 {
		t.Fatalf("the seller's list: %+v %v", theirs, listErr)
	}

	accounttest.Suspend(t, e.accounts, reporter)
	if _, _, err = e.trust.Report(ctx, reporter, service.ReportInput{AccountID: seller, Reason: domain.ReasonFake}); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
}

// A deleted listing cannot be reported; a hidden one can.
func TestReportListingStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	reporter, seller := e.person(t), e.person(t)
	hidden, deleted := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	if _, err := e.db.Exec(ctx, `UPDATE listings SET status = 'hidden' WHERE id = @id`, pgx.StrictNamedArgs{"id": hidden}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE listings SET status = 'deleted', deleted_at = now() WHERE id = @id`,
		pgx.StrictNamedArgs{"id": deleted}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.trust.Report(ctx, reporter, service.ReportInput{ListingID: hidden, Reason: domain.ReasonBanned}); err != nil {
		t.Fatalf("hidden: %v", err)
	}
	if _, _, err := e.trust.Report(ctx, reporter, service.ReportInput{ListingID: deleted, Reason: domain.ReasonBanned}); !errors.Is(err, catalogdomain.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

// A report of an account may name the pair's conversation, also after the
// reporter blocked the account; another pair's conversation or one beside a
// listing is invalid_report.
func TestReportFromAConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	reporter, seller, other := e.person(t), e.person(t), e.person(t)
	c := e.conversation(t, reporter, seller)
	theirs := e.conversation(t, other, seller)
	if err := e.accounts.Block(ctx, reporter, seller); err != nil {
		t.Fatal(err)
	}
	v, added, err := e.trust.Report(ctx, reporter, service.ReportInput{AccountID: seller, ConversationID: c, Reason: domain.ReasonScam})
	if err != nil || !added || v.ConversationID != c {
		t.Fatalf("report: %+v %v %v", v, added, err)
	}
	for name, in := range map[string]service.ReportInput{
		"another pair's":   {AccountID: seller, ConversationID: theirs, Reason: domain.ReasonScam},
		"an unknown one":   {AccountID: seller, ConversationID: uuid.NewV7(), Reason: domain.ReasonScam},
		"beside a listing": {ListingID: catalogtest.Listing(t, e.db, seller), ConversationID: c, Reason: domain.ReasonScam},
	} {
		if _, _, cerr := e.trust.Report(ctx, reporter, in); !errors.Is(cerr, domain.ErrInvalidReport) {
			t.Errorf("%s: %v", name, cerr)
		}
	}
	again, _, err := e.trust.Report(ctx, reporter, service.ReportInput{AccountID: seller, Reason: domain.ReasonScam})
	if err != nil || again.ID != v.ID || again.ConversationID != (uuid.UUID{}) {
		t.Fatalf("again without one: %+v %v", again, err)
	}
}

func TestReportingShopNexusIsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, _, err := e.trust.Report(context.Background(), e.person(t), service.ReportInput{AccountID: accountdomain.ShopNexusID, Reason: domain.ReasonScam})
	if !errors.Is(err, accountdomain.ErrSystemAccount) {
		t.Fatalf("got %v", err)
	}
}

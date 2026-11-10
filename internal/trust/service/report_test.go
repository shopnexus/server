package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/service"
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

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

// member inserts a console member and returns it as an actor.
func (e env) member(t *testing.T, name string) audit.Actor {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                             credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, @name, 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@console.test", "name": name, "at": time.Now()}); err != nil {
		t.Fatal(err)
	}
	return audit.Actor{Kind: audit.ActorMember, MemberID: id}
}

// A case shows its account, its most reported listing and its reporters;
// dismissing moves it to the resolved list, and reopening brings it back.
func TestReportCase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	lan := e.member(t, "Lan")
	seller, buyer, other := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	report := func(reporter uuid.UUID, in service.ReportInput) service.ReportView {
		t.Helper()
		v, _, err := e.trust.Report(ctx, reporter, in)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	report(buyer, service.ReportInput{ListingID: listing, Reason: domain.ReasonScam, Note: "Đòi cọc"})
	report(other, service.ReportInput{ListingID: listing, Reason: domain.ReasonScam})
	gone := report(other, service.ReportInput{AccountID: seller, Reason: domain.ReasonFake})
	if err := e.trust.WithdrawReport(ctx, other, gone.ID); err != nil {
		t.Fatal(err)
	}

	list, err := e.trust.ReportCases(ctx, domain.CaseOpen, page.Request{})
	if err != nil || list.Total == nil || *list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("open: %+v %v", list, err)
	}
	c := list.Items[0]
	if c.Account.ID != seller || c.Reports != 2 || c.Reasons[domain.ReasonScam] != 2 || len(c.Reasons) != 1 ||
		c.Listing == nil || c.Listing.ID != listing || c.Resolution != nil {
		t.Fatalf("the case: %+v", c)
	}
	d, err := e.trust.ReportCase(ctx, seller)
	if err != nil || len(d.Reports) != 2 || len(d.Listings) != 1 || d.Reports[1].Reporter.ID != buyer ||
		d.Reports[1].Note != "Đòi cọc" || d.Reports[1].ListingID != listing || d.Resolution != nil {
		t.Fatalf("detail: %+v %v", d, err)
	}

	d, err = e.trust.ResolveReportCase(ctx, seller, domain.OutcomeDismissed, lan)
	if err != nil || d.Resolution == nil || d.Resolution.Outcome != domain.OutcomeDismissed || d.Resolution.By.DisplayName != "Lan" ||
		d.Resolution.By.MemberID != lan.MemberID || len(d.Reports) != 2 {
		t.Fatalf("dismiss: %+v %v", d, err)
	}
	// Each reporter reads the case's reports as resolved, a withdrawn one
	// as withdrawn.
	mine, err := e.trust.MyReports(ctx, other, page.Request{})
	if err != nil || len(mine.Items) != 2 || mine.Items[0].ID != gone.ID || mine.Items[0].Status != domain.ReportWithdrawn ||
		mine.Items[1].Status != domain.ReportResolved {
		t.Fatalf("other's reports: %+v %v", mine, err)
	}
	if _, err = e.trust.ResolveReportCase(ctx, seller, domain.OutcomeActioned, lan); !errors.Is(err, domain.ErrCaseNotOpen) {
		t.Fatalf("resolve a resolved case: %v", err)
	}
	if list, err = e.trust.ReportCases(ctx, domain.CaseOpen, page.Request{}); err != nil || *list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("open after dismissing: %+v %v", list, err)
	}
	resolved, err := e.trust.ReportCases(ctx, domain.CaseResolved, page.Request{})
	if err != nil || resolved.Total != nil || len(resolved.Items) != 1 || resolved.Items[0].Resolution == nil {
		t.Fatalf("resolved: %+v %v", resolved, err)
	}

	if d, err = e.trust.ReopenReportCase(ctx, seller, lan); err != nil || d.Resolution != nil || len(d.Reports) != 2 {
		t.Fatalf("reopen: %+v %v", d, err)
	}
	if _, err = e.trust.ReopenReportCase(ctx, seller, lan); !errors.Is(err, domain.ErrCaseNotResolved) {
		t.Fatalf("reopen twice: %v", err)
	}
	if d, err = e.trust.ResolveReportCase(ctx, seller, domain.OutcomeActioned, lan); err != nil || d.Resolution.Outcome != domain.OutcomeActioned {
		t.Fatalf("resolve: %+v %v", d, err)
	}

	if _, err = e.trust.ReportCase(ctx, other); !errors.Is(err, domain.ErrCaseNotFound) {
		t.Fatalf("never reported: %v", err)
	}
	if _, err = e.trust.ResolveReportCase(ctx, other, domain.OutcomeDismissed, lan); !errors.Is(err, domain.ErrCaseNotOpen) {
		t.Fatalf("resolve without a case: %v", err)
	}
	for name, err := range map[string]error{
		"resolve": func() error {
			_, err := e.trust.ResolveReportCase(ctx, accountdomain.ShopNexusID, domain.OutcomeDismissed, lan)
			return err
		}(),
		"reopen": func() error { _, err := e.trust.ReopenReportCase(ctx, accountdomain.ShopNexusID, lan); return err }(),
	} {
		if !errors.Is(err, accountdomain.ErrSystemAccount) {
			t.Errorf("%s the system account: %v", name, err)
		}
	}
}

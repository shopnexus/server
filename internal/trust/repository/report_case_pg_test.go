package repository_test

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// member inserts a console member, whom a resolution names.
func member(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                             credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, @name, 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@console.test", "name": name, "at": time.Now()}); err != nil {
		t.Fatal(err)
	}
	return id
}

// Open cases group the reports not withdrawn by account, latest report
// first; resolving stamps them, a new report opens the next case, and
// reopening merges the resolved reports back, except one the same reporter
// has reported again.
func TestCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	reports, cases := repository.NewReports(db), repository.NewCases(db)
	seller, other := accounttest.New(t, db), accounttest.New(t, db)
	r1, r2, r3 := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	l1, l2 := catalogtest.Listing(t, db, seller), catalogtest.Listing(t, db, seller)
	lan := member(t, db, "Lan")
	start := time.Now().UTC().Truncate(time.Microsecond)
	minute := 0
	report := func(reporter, account, listing uuid.UUID, reason domain.Reason) domain.Report {
		t.Helper()
		minute++
		r, err := domain.NewReport(uuid.NewV7(), reporter, account, listing, uuid.UUID{}, reason, "", start.Add(time.Duration(minute)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		got, added, err := reports.Open(ctx, r)
		if err != nil || !added {
			t.Fatalf("open: %v %v", added, err)
		}
		return got
	}
	first := report(r1, seller, l1, domain.ReasonScam)
	report(r2, seller, l1, domain.ReasonFake)
	report(r3, seller, uuid.UUID{}, domain.ReasonOther)
	last := report(r1, seller, l2, domain.ReasonBanned)
	report(r1, other, uuid.UUID{}, domain.ReasonScam)
	withdrawn := report(r2, seller, uuid.UUID{}, domain.ReasonScam)
	if err := reports.Withdraw(ctx, withdrawn.ID(), start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	open, err := cases.Open(ctx, page.Request{Limit: 1})
	if err != nil || len(open) != 2 || open[0].AccountID != other || open[1].AccountID != seller {
		t.Fatalf("open: %+v %v", open, err)
	}
	next, err := cases.Open(ctx, page.Request{Limit: 1, Cursor: open[0].Key})
	if err != nil || len(next) != 1 || next[0].AccountID != seller {
		t.Fatalf("the next page: %+v %v", next, err)
	}
	c := next[0]
	want := map[domain.Reason]int{domain.ReasonScam: 1, domain.ReasonFake: 1, domain.ReasonOther: 1, domain.ReasonBanned: 1}
	if c.Key != last.ID() || c.Reports != 4 || !maps.Equal(c.Reasons, want) || !c.FirstAt.Equal(first.CreatedAt()) ||
		!c.LastAt.Equal(last.CreatedAt()) || c.Resolution != nil {
		t.Fatalf("the seller's case: %+v", c)
	}
	if got := domain.RankListings(c.Listings); len(got) != 2 || got[0] != l1 || got[1] != l2 {
		t.Fatalf("listings: %+v", c.Listings)
	}
	if n, countErr := cases.CountOpen(ctx); countErr != nil || n != 2 {
		t.Fatalf("count: %d %v", n, countErr)
	}
	if n, countErr := reports.CountOpen(ctx); countErr != nil || n != 5 {
		t.Fatalf("open reports: %d %v", n, countErr)
	}
	if n, countErr := cases.CountByStatus(ctx, time.Time{}); countErr != nil || !maps.Equal(n, map[domain.CaseStatus]int{domain.CaseOpen: 2}) {
		t.Fatalf("by status: %v %v", n, countErr)
	}
	// From the fifth report on, only other's case has one: the sixth was withdrawn.
	if n, _ := cases.CountByStatus(ctx, start.Add(5*time.Minute)); !maps.Equal(n, map[domain.CaseStatus]int{domain.CaseOpen: 1}) {
		t.Fatalf("since the fifth report: %v", n)
	}

	by := audit.Actor{Kind: audit.ActorMember, MemberID: lan}
	resolution := repository.Resolution{ID: uuid.NewV7(), Outcome: domain.OutcomeDismissed, MemberID: lan, At: start.Add(2 * time.Hour)}
	entry := audit.Entry{Actor: by, Action: audit.ReportDismiss, Target: audit.Account(seller), At: resolution.At}
	if err = cases.Resolve(ctx, seller, resolution, entry); err != nil {
		t.Fatal(err)
	}
	if err = cases.Resolve(ctx, seller, repository.Resolution{ID: uuid.NewV7(), Outcome: domain.OutcomeActioned, MemberID: lan, At: resolution.At},
		entry); !errors.Is(err, domain.ErrCaseNotOpen) {
		t.Fatalf("resolve twice: %v", err)
	}
	// The same outcome again is a retry whose first try landed: no new row.
	if err = cases.Resolve(ctx, seller, repository.Resolution{ID: uuid.NewV7(), Outcome: domain.OutcomeDismissed, MemberID: lan, At: resolution.At},
		entry); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n, _ := cases.CountOpen(ctx); n != 1 {
		t.Fatalf("count after resolving: %d", n)
	}
	if n, _ := reports.CountOpen(ctx); n != 1 {
		t.Fatalf("open reports after resolving: %d", n)
	}
	if n, _ := cases.CountByStatus(ctx, time.Time{}); !maps.Equal(n, map[domain.CaseStatus]int{domain.CaseOpen: 1, domain.CaseResolved: 1}) {
		t.Fatalf("by status after resolving: %v", n)
	}
	if n, _ := cases.CountByStatus(ctx, resolution.At.Add(time.Second)); len(n) != 0 {
		t.Fatalf("resolved before the cut: %v", n)
	}
	resolved, err := cases.Resolved(ctx, page.Request{})
	if err != nil || len(resolved) != 1 || resolved[0].AccountID != seller || resolved[0].Reports != 4 || resolved[0].Resolution == nil ||
		resolved[0].Resolution.Outcome != domain.OutcomeDismissed || resolved[0].Resolution.MemberName != "Lan" ||
		!resolved[0].Resolution.At.Equal(resolution.At) || len(resolved[0].Listings) != 2 {
		t.Fatalf("resolved: %+v %v", resolved, err)
	}
	current, rs, err := cases.Current(ctx, seller)
	if err != nil || current.Resolution == nil || current.Resolution.ID != resolution.ID || len(rs) != 4 || rs[0].ID() != last.ID() {
		t.Fatalf("current, resolved: %+v %d %v", current, len(rs), err)
	}

	// r1 reports l1 again: a new row, the next case.
	again := report(r1, seller, l1, domain.ReasonFake)
	if again.ID() == first.ID() {
		t.Fatal("the new report reused the resolved one")
	}
	current, rs, err = cases.Current(ctx, seller)
	if err != nil || current.Resolution != nil || current.Reports != 1 || len(rs) != 1 || rs[0].ID() != again.ID() {
		t.Fatalf("current, open: %+v %v %v", current, rs, err)
	}

	reopen := audit.Entry{Actor: by, Action: audit.ReportReopen, Target: audit.Account(seller), At: start.Add(3 * time.Hour)}
	if err = cases.Reopen(ctx, seller, reopen.At, reopen); err != nil {
		t.Fatal(err)
	}
	current, rs, err = cases.Current(ctx, seller)
	if err != nil || current.Resolution != nil || current.Reports != 4 || len(rs) != 4 {
		t.Fatalf("reopened: %+v %d %v", current, len(rs), err)
	}
	for _, r := range rs {
		if r.ID() == first.ID() {
			t.Fatal("the report r1 made again is back in the case")
		}
	}
	if resolved, _ = cases.Resolved(ctx, page.Request{}); len(resolved) != 0 {
		t.Fatalf("resolved after reopening: %+v", resolved)
	}
	if err = cases.Reopen(ctx, seller, reopen.At, reopen); !errors.Is(err, domain.ErrCaseNotResolved) {
		t.Fatalf("reopen twice: %v", err)
	}
	var kinds []string
	rows, err := db.Query(ctx, `SELECT action_kind || ' ' || coalesce(details->>'resolution_id', '') FROM console_actions
WHERE target_kind = 'account' AND target_id = @id ORDER BY id`, pgx.StrictNamedArgs{"id": seller})
	if err == nil {
		kinds, err = pgx.CollectRows(rows, pgx.RowTo[string])
	}
	if err != nil || len(kinds) != 2 || kinds[0] != "report.dismiss " || kinds[1] != "report.reopen "+resolution.ID.String() {
		t.Fatalf("audit: %v %v", kinds, err)
	}

	if _, _, err = cases.Current(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("never reported: %v", err)
	}
	stranger := uuid.NewV7()
	if err = cases.Resolve(ctx, stranger, repository.Resolution{ID: uuid.NewV7(), Outcome: domain.OutcomeDismissed, MemberID: lan, At: resolution.At},
		audit.Entry{Actor: by, Action: audit.ReportDismiss, Target: audit.Account(stranger), At: resolution.At}); !errors.Is(err, domain.ErrCaseNotOpen) {
		t.Fatalf("resolve an unknown account: %v", err)
	}
}

// Reopen picks the resolution Current shows: a later one whose reports were
// all withdrawn is no case, so the one before it reopens.
func TestReopenSkipsWithdrawnResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	reports, cases := repository.NewReports(db), repository.NewCases(db)
	seller, r1, r2 := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	lan := member(t, db, "Lan")
	by := audit.Actor{Kind: audit.ActorMember, MemberID: lan}
	start := time.Now().UTC().Truncate(time.Microsecond)
	resolve := func(reporter uuid.UUID, at time.Time) (domain.Report, repository.Resolution) {
		t.Helper()
		r, err := domain.NewReport(uuid.NewV7(), reporter, seller, uuid.UUID{}, uuid.UUID{}, domain.ReasonScam, "", at)
		if err != nil {
			t.Fatal(err)
		}
		if r, _, err = reports.Open(ctx, r); err != nil {
			t.Fatal(err)
		}
		res := repository.Resolution{ID: uuid.NewV7(), Outcome: domain.OutcomeDismissed, MemberID: lan, At: at.Add(time.Minute)}
		if err = cases.Resolve(ctx, seller, res, audit.Entry{Actor: by, Action: audit.ReportDismiss, Target: audit.Account(seller), At: res.At}); err != nil {
			t.Fatal(err)
		}
		return r, res
	}
	kept, older := resolve(r1, start)
	gone, _ := resolve(r2, start.Add(time.Hour))
	if err := reports.Withdraw(ctx, gone.ID(), start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if current, _, err := cases.Current(ctx, seller); err != nil || current.Resolution == nil || current.Resolution.ID != older.ID {
		t.Fatalf("current: %+v %v", current, err)
	}
	at := start.Add(3 * time.Hour)
	if err := cases.Reopen(ctx, seller, at, audit.Entry{Actor: by, Action: audit.ReportReopen, Target: audit.Account(seller), At: at}); err != nil {
		t.Fatal(err)
	}
	current, rs, err := cases.Current(ctx, seller)
	if err != nil || current.Resolution != nil || len(rs) != 1 || rs[0].ID() != kept.ID() {
		t.Fatalf("reopened: %+v %v %v", current, rs, err)
	}
}

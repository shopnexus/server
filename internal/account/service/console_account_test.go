package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

func TestConsoleAccounts(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if a, err := at(s, nil, now).ConsoleAccount(context.Background(), id); err != nil || a.ID != id || a.DisplayName != "Linh" {
		t.Fatalf("one: %+v %v", a, err)
	}
	newest := seed(t, s)
	first, err := at(s, nil, now).ConsoleAccounts(context.Background(), page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != newest || first.NextCursor == nil || *first.NextCursor != newest {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := at(s, nil, now).ConsoleAccounts(context.Background(), page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].ID != id || rest.NextCursor != nil {
		t.Fatalf("second page: %+v %v", rest, err)
	}
}

// SuspendedAccounts pages through the suspensions in force now, the one
// that ends soonest first, and SuspendedCount counts them.
func TestSuspendedAccounts(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	suspend := func(until *time.Time) {
		t.Helper()
		if _, err := at(s, nil, now).Suspend(ctx, service.SuspendInput{ID: seed(t, s), Reason: "spam", Until: until, By: audit.ByCLI("test")}); err != nil {
			t.Fatal(err)
		}
	}
	soon, later, lapsed := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(10*time.Minute)
	suspend(nil)
	suspend(&later)
	suspend(&soon)
	suspend(&lapsed)
	seed(t, s)
	svc := at(s, nil, now.Add(30*time.Minute))

	first, err := svc.SuspendedAccounts(ctx, page.Request{Limit: 2})
	if err != nil || len(first.Items) != 2 || !first.Items[0].SuspendedUntil.Equal(soon) || !first.Items[1].SuspendedUntil.Equal(later) ||
		first.Items[0].Status != domain.StatusSuspended || first.NextCursor == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := svc.SuspendedAccounts(ctx, page.Request{Cursor: *first.NextCursor, Limit: 2})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].SuspendedUntil != nil || rest.NextCursor != nil {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	if n, err := svc.SuspendedCount(ctx); err != nil || n != 3 {
		t.Fatalf("count: %d %v", n, err)
	}
}

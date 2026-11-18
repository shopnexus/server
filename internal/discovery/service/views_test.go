package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/service"
)

// A person's opens count once per UTC day: the week is the seven UTC days
// that end today, for the views and the saves alike.
// Every listing asked about has an entry. Erasure forgets what the person
// opened and keeps the counts.
func TestListingStats(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	e := newEnv(t, func(d *service.Deps) { d.Now = func() time.Time { return clock } })
	ctx := context.Background()
	seller, ann, bao := accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db)
	listing, quiet := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	view := func(who uuid.UUID) {
		t.Helper()
		if err := e.discovery.Viewed(ctx, who, listing); err != nil {
			t.Fatal(err)
		}
	}
	view(ann) // 2 November, more than a week before the read
	view(seller)
	clock = time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC)
	view(ann)
	view(ann)
	view(bao)
	if err := e.discovery.Save(ctx, bao, listing); err != nil {
		t.Fatal(err)
	}
	clock = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	got, err := e.discovery.ListingStats(ctx, []uuid.UUID{listing, quiet}, domain.ViewWeek(clock))
	if err != nil {
		t.Fatal(err)
	}
	if s := got[listing]; s != (service.ListingStats{Views: 3, ViewsSince: 2, Saves: 1, SavesSince: 1}) {
		t.Fatalf("listing: %+v", s)
	}
	if s, ok := got[quiet]; !ok || s != (service.ListingStats{}) {
		t.Fatalf("a quiet listing: %+v %v", s, ok)
	}

	if err = e.discovery.AccountDeleted(ctx, ann); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM listing_view_marks WHERE viewer_id = @ann`,
		pgx.StrictNamedArgs{"ann": ann}).Scan(&marks); err != nil || marks != 0 {
		t.Fatalf("marks after the erasure: %d %v", marks, err)
	}
	if got, err = e.discovery.ListingStats(ctx, []uuid.UUID{listing}, domain.ViewWeek(clock)); err != nil || got[listing].Views != 3 {
		t.Fatalf("counts after the erasure: %+v %v", got, err)
	}
}

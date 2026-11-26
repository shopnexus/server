package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// UC-DSC-03 A5, E1.
func TestSetAlertSavesAndTells(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	l := catalogtest.Listing(t, e.db, seller) // 1,500,000 đ
	if err := e.discovery.SetAlert(ctx, me, l, 1_500_000); err != nil {
		t.Fatal(err)
	}
	p, err := e.discovery.Saved(ctx, me, page.Request{})
	if err != nil || len(p.Items) != 1 || p.Items[0].AlertBelow == nil || *p.Items[0].AlertBelow != 1_500_000 {
		t.Fatalf("the alert saves the listing: %+v %v", p, err)
	}
	if _, err = e.db.Exec(ctx, `UPDATE listings SET price = 1400000 WHERE id = $1`, l); err != nil {
		t.Fatal(err)
	}
	if err = e.discovery.PriceMoved(ctx, l); err != nil {
		t.Fatal(err)
	}
	var save string
	if err = e.db.QueryRow(ctx, `SELECT subject_id::text FROM events WHERE kind = 'saved.price_dropped'`).Scan(&save); err != nil {
		t.Fatal(err)
	}
	f, found, err := e.discovery.PriceDropFacts(ctx, p.Items[0].ID)
	if err != nil || !found || f.Account != me || f.ListingID != l || f.Title != "Xe đạp Asama" || f.SavedPrice != 1_500_000 ||
		save != p.Items[0].ID.String() {
		t.Fatalf("%+v %v %v", f, found, err)
	}
	if err = e.discovery.ClearAlert(ctx, me, l); err != nil {
		t.Fatal(err)
	}
	if p, _ = e.discovery.Saved(ctx, me, page.Request{}); len(p.Items) != 1 || p.Items[0].AlertBelow != nil {
		t.Fatalf("cleared, still saved: %+v", p)
	}
	if err = e.discovery.SetAlert(ctx, seller, l, 1_000_000); !errors.Is(err, domain.ErrOwnListing) {
		t.Fatalf("own: %v", err)
	}
}

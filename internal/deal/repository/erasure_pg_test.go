package repository_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestOpenOrdersActiveOfAndScrub(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	buyer, seller := accounttest.New(t, db), accounttest.New(t, db)
	d := pendingDeal(t, db, s, buyer, seller)

	open, err := s.OpenOrders(ctx, buyer)
	if err != nil || !open {
		t.Fatalf("buyer's open orders: %v %v", open, err)
	}
	if open, _ = s.OpenOrders(ctx, seller); !open {
		t.Fatal("seller's open orders")
	}
	if open, _ = s.OpenOrders(ctx, uuid.NewV7()); open {
		t.Fatal("a stranger has open orders")
	}
	active, err := s.ActiveOf(ctx, buyer)
	if err != nil || len(active) != 1 || active[0].ID() != d.ID() {
		t.Fatalf("active: %v %v", len(active), err)
	}
	if active, _ = s.ActiveOf(ctx, seller); len(active) != 1 {
		t.Fatalf("seller's active deals: %d", len(active))
	}
	if err = s.ScrubRecipients(ctx, buyer); err != nil {
		t.Fatal(err)
	}
	var name, phone, street, commune string
	if err = db.QueryRow(ctx, `SELECT recipient_name, recipient_phone, recipient_street, recipient_commune FROM orders WHERE deal_id = @id`,
		pgx.StrictNamedArgs{"id": d.ID()}).Scan(&name, &phone, &street, &commune); err != nil {
		t.Fatal(err)
	}
	if name != "" || phone != "" || street != "" || commune == "" {
		t.Fatalf("after scrub: %q %q %q %q", name, phone, street, commune)
	}
}

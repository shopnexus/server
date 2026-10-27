package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// Moves reads the moves a notification names, with the people and the
// listing, and leaves out an ID the deal lacks.
func TestMoves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	l := e.listing(t, seller)
	v := e.open(t, buyer, l)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	var offer uuid.UUID
	if err := e.db.QueryRow(ctx, `SELECT id FROM deal_moves WHERE deal_id = @id AND kind = 'offer'`,
		pgx.StrictNamedArgs{"id": v.ID}).Scan(&offer); err != nil {
		t.Fatal(err)
	}
	f, ok, err := e.deals.Moves(ctx, v.ID, []uuid.UUID{offer, uuid.NewV7()})
	if err != nil || !ok || f.BuyerID != buyer || f.SellerID != seller || f.ListingID != l || f.ListingTitle == "" ||
		f.BuyerName == "" || len(f.Moves) != 1 || f.Moves[0].Kind != domain.MoveOffer || f.Moves[0].By != domain.Buyer ||
		f.Moves[0].Amount != 1_000_000 {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.deals.Moves(ctx, uuid.NewV7(), nil); err != nil || ok {
		t.Fatalf("an unknown deal: %v %v", ok, err)
	}
}

package service_test

import (
	"context"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// A deal opens the pair's conversation, and every deal of the pair, either
// way, lives in it.
func TestOpenOpensThePairsConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	first := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	conv, err := e.chats.ConversationWith(ctx, buyer, seller)
	if err != nil || conv == (uuid.UUID{}) || first.ConversationID != conv {
		t.Fatalf("the first deal's conversation %s, the pair's %s %v", first.ConversationID, conv, err)
	}
	second := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	back := e.open(t, seller, catalogtest.Listing(t, e.db, buyer))
	if second.ConversationID != conv || back.ConversationID != conv {
		t.Fatalf("second %s, the other way %s, want %s", second.ConversationID, back.ConversationID, conv)
	}
	s, ok, err := e.deals.BuyerSummary(ctx, buyer, first.Listing.ID)
	if err != nil || !ok || s.ConversationID != conv {
		t.Fatalf("summary: %+v %v %v", s, ok, err)
	}
	if got := e.get(t, seller, first.ID); got.ConversationID != conv {
		t.Fatalf("the seller reads %s", got.ConversationID)
	}
}

// PostMoves writes a message for each move a person made and each order
// step, the system's with ShopNexus as the sender, and none for a change
// of the listing; a second post writes nothing more.
func TestPostMoves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	bike := catalogtest.Listing(t, e.db, seller)
	sold := e.shipped(t, buyer, seller, bike, "SPXCHAT1")
	if err := e.deals.ShipmentUpdated(ctx, "SPXCHAT1", "in_transit"); err != nil {
		t.Fatal(err)
	}
	lamp := catalogtest.Listing(t, e.db, seller)
	offered := e.open(t, buyer, lamp)
	must(t)(e.deals.Offer(ctx, buyer, offered.ID, 1_000_000))
	e.edit(t, lamp, func(in *catalogdomain.ListingInput) { in.Price = 1_400_000 })
	if err := e.deals.ListingChanged(ctx, lamp); err != nil {
		t.Fatal(err)
	}

	moves, err := e.repo.Moves(ctx, []uuid.UUID{sold.ID, offered.ID})
	if err != nil {
		t.Fatal(err)
	}
	senders := map[domain.MoveKind]uuid.UUID{
		domain.MoveAccept: buyer, domain.MoveCodRequested: buyer, domain.MoveOffer: buyer, domain.MoveShipped: seller,
		domain.MoveShipmentUpdated: accountdomain.ShopNexusID,
	}
	want := map[uuid.UUID]uuid.UUID{} // move → sender
	for _, d := range []uuid.UUID{sold.ID, offered.ID} {
		ids := make([]uuid.UUID, 0, len(moves[d]))
		for _, m := range moves[d] {
			ids = append(ids, m.ID)
			if sender, ok := senders[m.Kind]; ok {
				want[m.ID] = sender
			}
		}
		for range 2 {
			if err = e.deals.PostMoves(ctx, d, ids); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(want) != 5 {
		t.Fatalf("%d moves to post", len(want))
	}
	p, err := e.chats.Messages(ctx, buyer, sold.ConversationID, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != len(want) {
		t.Fatalf("%d messages, want %d", len(p.Items), len(want))
	}
	for _, m := range p.Items {
		if sender, ok := want[m.ID]; !ok || m.SenderID != sender || m.Deal == nil || m.Deal.Move.Kind == "price_changed" {
			t.Errorf("%+v %+v", m, m.Deal)
		}
	}
	if err = e.deals.PostMoves(ctx, uuid.NewV7(), nil); err != nil {
		t.Fatalf("an unknown deal: %v", err)
	}
}

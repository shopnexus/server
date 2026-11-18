package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

// A deal opens the pair's conversation, and every deal of the pair, either
// way, lives in it.
//
// UC-DEAL-01 · UC-DEAL-02.
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
//
// UC-DEAL-01.
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

// failingInsert is a store whose Insert fails, as a lost connection does.
type failingInsert struct{ repository.Deals }

func (failingInsert) Insert(context.Context, domain.Deal) (bool, error) {
	return false, errors.New("connection lost")
}

// A deal that could not be stored leaves no conversation it opened, and
// keeps one the pair had.
func TestOpenThatFailsLeavesNoConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deps := e.deps
	deps.Deals = failingInsert{e.repo}
	deals := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	if _, _, err := deals.Open(ctx, buyer, catalogtest.Listing(t, e.db, seller)); err == nil {
		t.Fatal("the open succeeded")
	}
	if conv, err := e.chats.ConversationWith(ctx, buyer, seller); err != nil || conv != (uuid.UUID{}) {
		t.Fatalf("a conversation stayed: %s %v", conv, err)
	}
	had := e.open(t, buyer, catalogtest.Listing(t, e.db, seller)).ConversationID
	if _, _, err := deals.Open(ctx, buyer, catalogtest.Listing(t, e.db, seller)); err == nil {
		t.Fatal("the second open succeeded")
	}
	if conv, err := e.chats.ConversationWith(ctx, buyer, seller); err != nil || conv != had {
		t.Fatalf("the pair's conversation: %s %v, want %s", conv, err, had)
	}
}

// strayParty is a store whose moves carry a party the deal does not know.
type strayParty struct{ repository.Deals }

func (s strayParty) MovesOf(ctx context.Context, deal uuid.UUID, ids []uuid.UUID) ([]domain.Move, error) {
	moves, err := s.Deals.MovesOf(ctx, deal, ids)
	for i := range moves {
		if moves[i].Kind == domain.MoveOffer {
			moves[i].By = "admin"
		}
	}
	return moves, err
}

// A move of no party is skipped and the others are posted.
func TestPostMovesSkipsAStrayParty(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deps := e.deps
	deps.Deals = strayParty{e.repo}
	deals := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	d := e.open(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.Offer(ctx, buyer, d.ID, 1_000_000))
	must(t)(e.deals.Offer(ctx, seller, d.ID, 1_300_000))
	moves, err := e.repo.Moves(ctx, []uuid.UUID{d.ID})
	if err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{moves[d.ID][0].ID, moves[d.ID][1].ID}
	if err = deals.PostMoves(ctx, d.ID, ids); err != nil {
		t.Fatal(err)
	}
	p, err := e.chats.Messages(ctx, buyer, d.ConversationID, page.Request{})
	if err != nil || len(p.Items) != 1 || p.Items[0].ID != ids[1] {
		t.Fatalf("%+v %v", p.Items, err)
	}
}

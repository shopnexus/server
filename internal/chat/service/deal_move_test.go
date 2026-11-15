package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// deal stores a deal of buyer's on a listing of seller's in conversation,
// as module deal does, so a deal message has a deal to name.
func (e env) deal(t *testing.T, conversation, buyer, seller uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO deals (id, conversation_id, listing_id, buyer_id, seller_id, status,
    listed_price, write_id, created_at, updated_at)
VALUES (@id, @conversation, @listing, @buyer, @seller, 'negotiating', 1500000, @id, now(), now())`, pgx.StrictNamedArgs{
		"id": id, "conversation": conversation, "listing": catalogtest.Listing(t, e.db, seller), "buyer": buyer, "seller": seller,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// unread reads viewer's unread count in their inbox's only row, 0 when the
// inbox does not list the conversation.
func (e env) unread(t *testing.T, viewer uuid.UUID) int {
	t.Helper()
	p, err := e.chat.Inbox(context.Background(), viewer, false, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) == 0 {
		return 0
	}
	return p.Items[0].Unread
}

func TestOpenPair(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	a, b := e.person(t), e.person(t)
	id, err := e.chat.OpenPair(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.chat.OpenPair(ctx, b, a)
	if err != nil || again != id {
		t.Fatalf("the other way: %s %v, want %s", again, err, id)
	}
	if with, err := e.chat.ConversationWith(ctx, a, b); err != nil || with != id {
		t.Fatalf("with: %s %v", with, err)
	}
	// A conversation without a message stays out of the inbox.
	if n := e.unread(t, a); n != 0 {
		t.Fatalf("unread %d", n)
	}
	if p, err := e.chat.Inbox(ctx, a, false, page.Request{}); err != nil || len(p.Items) != 0 {
		t.Fatalf("inbox: %+v %v", p.Items, err)
	}
}

// A move's message reads like a sent message: both members see it, the one
// who did not move counts it unread, and the conversation leads the inbox.
// The system's move counts for both. A repeated post writes nothing more,
// no move writes message.sent, and a block stops none.
func TestPostMove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	conv, err := e.chat.OpenPair(ctx, buyer, seller)
	if err != nil {
		t.Fatal(err)
	}
	deal := e.deal(t, conv, buyer, seller)
	at := time.Now().Truncate(time.Microsecond)
	offer := service.MoveInput{
		ID: uuid.NewV7(), ConversationID: conv, DealID: deal, SenderID: buyer, Kind: "offer", By: "buyer", Amount: 1_000_000, At: at,
	}
	for range 2 {
		if err = e.chat.PostMove(ctx, offer); err != nil {
			t.Fatal(err)
		}
	}
	for _, viewer := range []uuid.UUID{buyer, seller} {
		ms := e.messages(t, viewer, conv)
		if len(ms) != 1 {
			t.Fatalf("%d messages", len(ms))
		}
		m := ms[0]
		if m.ID != offer.ID || m.SenderID != buyer || m.Text != "" || len(m.Photos) != 0 || m.Status != domain.StatusSent ||
			m.Deal == nil || m.Deal.DealID != deal || m.Deal.Move.Kind != "offer" || m.Deal.Move.By != "buyer" ||
			m.Deal.Move.Amount == nil || *m.Deal.Move.Amount != 1_000_000 || !m.Deal.Move.At.Equal(at) || !m.CreatedAt.Equal(at) {
			t.Fatalf("%+v %+v", m, m.Deal)
		}
	}
	if b, s := e.unread(t, buyer), e.unread(t, seller); b != 0 || s != 1 {
		t.Fatalf("unread: buyer %d, seller %d", b, s)
	}
	if k := pgtest.EventKinds(t, e.db, offer.ID); len(k) != 0 {
		t.Fatalf("events %v", k)
	}

	update := service.MoveInput{
		ID: uuid.NewV7(), ConversationID: conv, DealID: deal, SenderID: accountdomain.ShopNexusID, Kind: "shipment_updated",
		By: "system", At: at.Add(time.Second),
	}
	if err = e.chat.PostMove(ctx, update); err != nil {
		t.Fatal(err)
	}
	if b, s := e.unread(t, buyer), e.unread(t, seller); b != 1 || s != 2 {
		t.Fatalf("unread after the system's move: buyer %d, seller %d", b, s)
	}
	if m := e.messages(t, buyer, conv)[0]; m.ID != update.ID || m.Deal == nil || m.Deal.Move.Amount != nil {
		t.Fatalf("%+v", m)
	}

	if err = e.accounts.Block(ctx, seller, buyer); err != nil {
		t.Fatal(err)
	}
	withdraw := service.MoveInput{
		ID: uuid.NewV7(), ConversationID: conv, DealID: deal, SenderID: buyer, Kind: "withdraw", By: "buyer", At: at.Add(2 * time.Second),
	}
	if err = e.chat.PostMove(ctx, withdraw); err != nil {
		t.Fatalf("a move across a block: %v", err)
	}
	if ms := e.messages(t, seller, conv); len(ms) != 3 || ms[0].ID != withdraw.ID {
		t.Fatalf("the blocker's thread: %d", len(ms))
	}
	if n := e.unread(t, seller); n != 0 {
		t.Fatalf("the blocker's inbox lists the conversation: %d", n)
	}

	stranger := offer
	stranger.ID, stranger.SenderID = uuid.NewV7(), e.person(t)
	if err = e.chat.PostMove(ctx, stranger); err == nil {
		t.Fatal("a stranger's move was posted")
	}
}

func TestOtherRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không bạn?")
	if got, err := e.chat.OtherRead(ctx, buyer, c.ID); err != nil || got != nil {
		t.Fatalf("before a read: %v %v", got, err)
	}
	first := e.messages(t, seller, c.ID)[0]
	if err := e.chat.Read(ctx, seller, c.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := e.chat.OtherRead(ctx, buyer, c.ID); err != nil || got == nil || *got != first.ID {
		t.Fatalf("after the seller read: %v %v", got, err)
	}
	if got, err := e.chat.OtherRead(ctx, seller, c.ID); err != nil || got != nil {
		t.Fatalf("the buyer read nothing: %v %v", got, err)
	}
	if _, err := e.chat.OtherRead(ctx, e.person(t), c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
}

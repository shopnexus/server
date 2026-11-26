package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// dealIn stores a deal of buyer's on a listing of seller's in
// conversation, as module deal does, so a deal message has a deal to name.
func dealIn(t *testing.T, db *pgxpool.Pool, conversation, buyer, seller uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO deals (id, conversation_id, listing_id, buyer_id, seller_id, status,
    listed_price, write_id, created_at, updated_at)
VALUES (@id, @conversation, @listing, @buyer, @seller, 'negotiating', 1500000, @id, now(), now())`, pgx.StrictNamedArgs{
		"id": id, "conversation": conversation, "listing": catalogtest.Listing(t, db, seller), "buyer": buyer, "seller": seller,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// unread reads viewer's unread count in their inbox's only row, 0 when the
// inbox does not list the conversation.
func (e env) unread(t *testing.T, viewer uuid.UUID) int {
	t.Helper()
	p, err := e.chat.Inbox(context.Background(), viewer, domain.BoxInbox, page.Request{})
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
	id, opened, err := e.chat.OpenPair(ctx, a, b)
	if err != nil || !opened {
		t.Fatalf("open: %v %v", opened, err)
	}
	again, opened, err := e.chat.OpenPair(ctx, b, a)
	if err != nil || again != id || opened {
		t.Fatalf("the other way: %s %v %v, want %s", again, opened, err, id)
	}
	if with, err := e.chat.ConversationWith(ctx, a, b); err != nil || with != id {
		t.Fatalf("with: %s %v", with, err)
	}
	// A conversation without a message stays out of the inbox.
	if n := e.unread(t, a); n != 0 {
		t.Fatalf("unread %d", n)
	}
	if p, err := e.chat.Inbox(ctx, a, domain.BoxInbox, page.Request{}); err != nil || len(p.Items) != 0 {
		t.Fatalf("inbox: %+v %v", p.Items, err)
	}
}

// DropPair deletes a conversation nothing uses yet and keeps one with a
// message.
func TestDropPair(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	a, b := e.person(t), e.person(t)
	id, _, err := e.chat.OpenPair(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.chat.DropPair(ctx, id); err != nil {
		t.Fatal(err)
	}
	if with, withErr := e.chat.ConversationWith(ctx, a, b); withErr != nil || with != (uuid.UUID{}) {
		t.Fatalf("dropped: %s %v", with, withErr)
	}
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không bạn?")
	if err = e.chat.DropPair(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if ms := e.messages(t, seller, c.ID); len(ms) != 1 {
		t.Fatalf("a conversation with a message was dropped: %d", len(ms))
	}
}

// A move's message reads like a sent message: both members see it, the one
// who did not move counts it unread, and the conversation leads the inbox.
// The system's move counts for both. A repeated post writes nothing more,
// no move writes message.sent, and a block stops none.
//
// UC-DEAL-01.
func TestPostMove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	conv, _, err := e.chat.OpenPair(ctx, buyer, seller)
	if err != nil {
		t.Fatal(err)
	}
	deal := dealIn(t, e.db, conv, buyer, seller)
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
	nowhere := offer
	nowhere.ID, nowhere.ConversationID = uuid.NewV7(), uuid.NewV7()
	for name, in := range map[string]service.MoveInput{"a stranger's move": stranger, "an unknown conversation": nowhere} {
		if err = e.chat.PostMove(ctx, in); !errors.Is(err, domain.ErrMisplacedMove) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A deal message joins the thread and tells the streams but brings the
// conversation back from no archive, whether it lands older than the last
// message, as a late event does, or newest: only a message someone writes
// does.
//
// UC-DEAL-01 A3.
func TestMovesKeepTheArchive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	late := uuid.NewV7() // minted before the text, so older than it
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không bạn?")
	deal := dealIn(t, e.db, c.ID, buyer, seller)
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	archived := func() bool {
		t.Helper()
		v, err := e.chat.Get(ctx, seller, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v.Archived
	}
	move := service.MoveInput{
		ID: late, ConversationID: c.ID, DealID: deal, SenderID: buyer, Kind: "offer", By: "buyer", Amount: 1_000_000, At: time.Now(),
	}
	if err := e.chat.PostMove(ctx, move); err != nil {
		t.Fatal(err)
	}
	if ms := e.messages(t, seller, c.ID); len(ms) != 2 || ms[1].ID != late || !archived() {
		t.Fatalf("a late move: %d messages, archived %v", len(ms), archived())
	}
	move.ID, move.Kind = uuid.NewV7(), "withdraw"
	if err := e.chat.PostMove(ctx, move); err != nil {
		t.Fatal(err)
	}
	if ms := e.messages(t, seller, c.ID); len(ms) != 3 || !archived() {
		t.Fatalf("the newest move: %d messages, archived %v", len(ms), archived())
	}
}

// The conversation shows the other member's read marker, absent before
// they read anything.
//
// UC-DEAL-01.
func TestGetShowsTheOtherRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không bạn?")
	if c.OtherRead != nil {
		t.Fatalf("before a read: %v", c.OtherRead)
	}
	first := e.messages(t, seller, c.ID)[0]
	if err := e.chat.Read(ctx, seller, c.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := e.chat.Get(ctx, buyer, c.ID); err != nil || got.OtherRead == nil || *got.OtherRead != first.ID {
		t.Fatalf("after the seller read: %+v %v", got, err)
	}
	if got, err := e.chat.Get(ctx, seller, c.ID); err != nil || got.OtherRead != nil {
		t.Fatalf("the buyer read nothing: %+v %v", got, err)
	}
	if _, err := e.chat.Get(ctx, e.person(t), c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
}

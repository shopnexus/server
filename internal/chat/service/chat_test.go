package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// UC-DEAL-01.
func TestOpenAndSend(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	first, second := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	c := e.open(t, buyer, first, "Còn không bạn?")
	if c.Other.ID != seller || c.Other.DisplayName == "" {
		t.Fatalf("%+v", c)
	}
	ms := e.messages(t, seller, c.ID)
	if len(ms) != 1 || ms[0].Text != "Còn không bạn?" || ms[0].ListingID == nil || *ms[0].ListingID != first || ms[0].Status != domain.StatusSent {
		t.Fatalf("%+v", ms)
	}
	if again := e.open(t, buyer, second, "Món này nữa"); again.ID != c.ID {
		t.Fatalf("a second conversation %s", again.ID)
	}
	reply, err := e.chat.Send(ctx, seller, c.ID, key(), service.SendInput{Text: "Còn bạn ơi"})
	if err != nil || reply.SenderID != seller {
		t.Fatalf("%+v %v", reply, err)
	}
	for _, who := range []uuid.UUID{buyer, seller} {
		p, inboxErr := e.chat.Inbox(ctx, who, domain.BoxInbox, page.Request{})
		if inboxErr != nil || len(p.Items) != 1 || p.Items[0].LastMessage.ID != reply.ID || p.Items[0].Other.ID == who {
			t.Fatalf("inbox of %s: %+v %v", who, p, inboxErr)
		}
	}
	if n, _ := e.chat.Unread(ctx, buyer); n != 1 {
		t.Fatalf("buyer unread %d", n)
	}
	if err = e.chat.Read(ctx, buyer, c.ID, reply.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.chat.Unread(ctx, buyer); n != 0 {
		t.Fatalf("unread after reading %d", n)
	}
}

// UC-DEAL-01 E1, E2, E3, E4, E5.
func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	hidden := catalogtest.Listing(t, e.db, seller)
	if _, err := e.catalog.Hide(ctx, seller, hidden, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	suspended := e.person(t)
	accounttest.Suspend(t, e.accounts, suspended)
	blocked := e.person(t)
	if err := e.accounts.Block(ctx, seller, blocked); err != nil {
		t.Fatal(err)
	}
	blocking := e.person(t)
	if err := e.accounts.Block(ctx, blocking, seller); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name    string
		buyer   uuid.UUID
		listing uuid.UUID
		want    error
	}{
		{"suspended", suspended, listing, accountdomain.ErrSuspended},
		{"own listing", seller, listing, domain.ErrOwnListing},
		{"hidden listing", e.person(t), hidden, domain.ErrListingUnavailable},
		{"unknown listing", e.person(t), uuid.NewV7(), catalogdomain.ErrNotFound},
		{"blocked by the seller", blocked, listing, accountdomain.ErrBlocked},
		{"blocking the seller", blocking, listing, accountdomain.ErrBlocked},
	} {
		if _, err := e.chat.Open(ctx, c.buyer, key(), service.OpenInput{ListingID: c.listing, Text: "Còn không?"}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := e.chat.Open(ctx, accounttest.New(t, e.db), key(), service.OpenInput{ListingID: listing, Text: "Còn không?"}); !errors.Is(err, accountdomain.ErrPhoneRequired) {
		t.Fatalf("an account without a phone opens a chat: %v", err)
	}
}

// UC-DEAL-01 E1, E7.
func TestSendRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if _, err := e.chat.Send(ctx, stranger, c.ID, key(), service.SendInput{Text: "Hi"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "   "}); !errors.Is(err, domain.ErrInvalidMessage) {
		t.Errorf("empty: %v", err)
	}
	strangers := catalogtest.Listing(t, e.db, stranger)
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Món này?", ListingID: &strangers}); !errors.Is(err, domain.ErrInvalidMessage) {
		t.Errorf("a listing of neither member: %v", err)
	}
	if err := e.accounts.Block(ctx, seller, buyer); err != nil {
		t.Fatal(err)
	}
	for _, who := range []uuid.UUID{buyer, seller} {
		if _, err := e.chat.Send(ctx, who, c.ID, key(), service.SendInput{Text: "Hi"}); !errors.Is(err, accountdomain.ErrBlocked) {
			t.Errorf("blocked: %v", err)
		}
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 0 {
		t.Errorf("the blocker still lists it: %+v", p.Items)
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.chat.Get(ctx, seller, c.ID)
	if err != nil || got.ID != c.ID || got.Other.ID != buyer || !got.Archived {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = e.chat.Get(ctx, e.person(t), c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
}

// UC-DEAL-01 A3.
func TestArchive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxArchived, page.Request{}); len(p.Items) != 1 || !p.Items[0].Archived {
		t.Fatalf("archived: %+v", p.Items)
	}
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Bạn ơi"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("not back: %+v", p.Items)
	}
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.chat.Unarchive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("unarchive: %+v", p.Items)
	}
}

// A deal's move does not bring an archived conversation back: only a
// message someone wrote does.
//
// UC-DEAL-01 A3.
func TestArchiveStaysOnADealMove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	move := service.MoveInput{
		ID: uuid.NewV7(), ConversationID: c.ID, DealID: dealIn(t, e.db, c.ID, buyer, seller), SenderID: buyer,
		Kind: "offer", By: "buyer", Amount: 900_000, At: time.Now(),
	}
	if err := e.chat.PostMove(ctx, move); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxArchived, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("a move brought it back: %+v", p.Items)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 0 {
		t.Fatalf("in the inbox: %+v", p.Items)
	}
}

// An ignored conversation lists under BoxIgnored alone, counts as unread
// nowhere and pushes nothing, and the other member's messages leave it
// there. Writing to it, or Unignore, brings it back.
//
// UC-DEAL-01 A3.
func TestIgnore(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if err := e.chat.Ignore(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Bạn ơi, 500k nhé"})
	if err != nil {
		t.Fatal(err)
	}
	for _, box := range []domain.Box{domain.BoxInbox, domain.BoxArchived} {
		if p, _ := e.chat.Inbox(ctx, seller, box, page.Request{}); len(p.Items) != 0 {
			t.Fatalf("%s lists it: %+v", box, p.Items)
		}
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxIgnored, page.Request{}); len(p.Items) != 1 || !p.Items[0].Ignored {
		t.Fatalf("ignored: %+v", p.Items)
	}
	if n, _ := e.chat.Unread(ctx, seller); n != 0 {
		t.Fatalf("unread %d", n)
	}
	if f, _, _ := e.chat.MessageFacts(ctx, sent.ID); !f.Ignored {
		t.Fatalf("facts: %+v", f)
	}
	if v, _ := e.chat.Get(ctx, seller, c.ID); !v.Ignored {
		t.Fatalf("view: %+v", v)
	}
	if on, _ := e.chat.Ignores(ctx, seller, buyer); !on {
		t.Fatal("seller ignores buyer")
	}
	if on, _ := e.chat.Ignores(ctx, buyer, seller); on {
		t.Fatal("the buyer ignores no one")
	}
	if _, err = e.chat.Send(ctx, seller, c.ID, key(), service.SendInput{Text: "Không bán giá đó bạn"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("writing did not bring it back: %+v", p.Items)
	}
	if err = e.chat.Ignore(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.chat.Unignore(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("unignore: %+v", p.Items)
	}
}

func TestConversationWith(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	for _, p := range [][2]uuid.UUID{{buyer, seller}, {seller, buyer}} {
		if id, err := e.chat.ConversationWith(ctx, p[0], p[1]); err != nil || id != c.ID {
			t.Fatalf("%v: %v %v", p, id, err)
		}
	}
	if id, err := e.chat.ConversationWith(ctx, stranger, seller); err != nil || id != (uuid.UUID{}) {
		t.Fatalf("no conversation: %v %v", id, err)
	}
}

func TestGetShowsWhenTheOtherWasActive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	got, err := e.chat.Get(ctx, seller, c.ID)
	if err != nil || got.Other.LastActiveAt != nil {
		t.Fatalf("before: %+v %v", got.Other, err)
	}
	if err = e.accounts.Touch(ctx, []uuid.UUID{buyer}); err != nil {
		t.Fatal(err)
	}
	if got, err = e.chat.Get(ctx, seller, c.ID); err != nil || got.Other.LastActiveAt == nil {
		t.Fatalf("after: %+v %v", got.Other, err)
	}
	inbox, err := e.chat.Inbox(ctx, seller, domain.BoxInbox, page.Request{})
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].Other.LastActiveAt == nil {
		t.Fatalf("inbox: %+v %v", inbox, err)
	}
}

func TestPeer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if other, ok, err := e.chat.Peer(ctx, seller, c.ID); err != nil || !ok || other != buyer {
		t.Fatalf("member: %s %v %v", other, ok, err)
	}
	if _, ok, err := e.chat.Peer(ctx, e.person(t), c.ID); err != nil || ok {
		t.Fatalf("stranger: %v %v", ok, err)
	}
	if _, ok, err := e.chat.Peer(ctx, seller, uuid.NewV7()); err != nil || ok {
		t.Fatalf("unknown conversation: %v %v", ok, err)
	}
	if err := e.accounts.Block(ctx, seller, buyer); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.chat.Peer(ctx, buyer, c.ID); err != nil || ok {
		t.Fatalf("blocked: %v %v", ok, err)
	}
}

// UC-DEAL-01 A3.
func TestMarkUnread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không bạn?")
	if err := e.chat.MarkUnread(ctx, buyer, c.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.chat.Unread(ctx, buyer); n != 1 {
		t.Fatalf("buyer's unread: %d", n)
	}
	if err := e.chat.MarkUnread(ctx, stranger, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
	if err := e.chat.MarkUnread(ctx, buyer, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

// Only opening needs a phone: a seller without one replies in a chat a
// buyer with a phone opened on its listing.
//
// UC-DEAL-01 E2.
func TestPhoneRequiredOnlyToOpen(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	phoneless := accounttest.New(t, e.db)
	c := e.open(t, e.person(t), catalogtest.Listing(t, e.db, phoneless), "Còn không?")
	if _, err := e.chat.Send(ctx, phoneless, c.ID, key(), service.SendInput{Text: "Còn bạn"}); err != nil {
		t.Fatalf("a reply without a phone: %v", err)
	}
}

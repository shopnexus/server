package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

func TestOpenAndSend(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	first, second := e.listing(t, seller), e.listing(t, seller)
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
		p, inboxErr := e.chat.Inbox(ctx, who, false, page.Request{})
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

func TestOpenRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	listing := e.listing(t, seller)
	hidden := e.listing(t, seller)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	suspended := e.person(t)
	e.suspend(t, suspended)
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
		{"no phone", e.emailOnly(t), listing, domain.ErrPhoneRequired},
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
}

func TestSendRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	if _, err := e.chat.Send(ctx, stranger, c.ID, key(), service.SendInput{Text: "Hi"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "   "}); !errors.Is(err, domain.ErrInvalidMessage) {
		t.Errorf("empty: %v", err)
	}
	strangers := e.listing(t, stranger)
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
	if p, _ := e.chat.Inbox(ctx, seller, false, page.Request{}); len(p.Items) != 0 {
		t.Errorf("the blocker still lists it: %+v", p.Items)
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
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

func TestArchive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, true, page.Request{}); len(p.Items) != 1 || !p.Items[0].Archived {
		t.Fatalf("archived: %+v", p.Items)
	}
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Bạn ơi"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, false, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("not back: %+v", p.Items)
	}
	if err := e.chat.Archive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.chat.Unarchive(ctx, seller, c.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.chat.Inbox(ctx, seller, false, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("unarchive: %+v", p.Items)
	}
}

func TestConversationWith(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	for _, p := range [][2]uuid.UUID{{buyer, seller}, {seller, buyer}} {
		if id, err := e.chat.ConversationWith(ctx, p[0], p[1]); err != nil || id != c.ID {
			t.Fatalf("%v: %v %v", p, id, err)
		}
	}
	if id, err := e.chat.ConversationWith(ctx, stranger, seller); err != nil || id != (uuid.UUID{}) {
		t.Fatalf("no conversation: %v %v", id, err)
	}
}

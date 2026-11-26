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
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// conversation has a new buyer open a chat on a new seller's listing.
func (e env) conversation(t *testing.T) (c service.ConversationView, buyer, seller uuid.UUID) {
	t.Helper()
	buyer, seller = e.person(t), e.person(t)
	return e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn bảo hành không?"), buyer, seller
}

// UC-DEAL-01 E7.
func TestReplyToMustBeASentMessageHere(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c, buyer, seller := e.conversation(t)
	elsewhere, _, _ := e.conversation(t)
	foreign := e.messages(t, elsewhere.Other.ID, elsewhere.ID)[0]
	photo, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Photos: []uuid.UUID{e.upload(t, buyer, []byte("not a jpeg at all"))}})
	if err != nil {
		t.Fatal(err)
	}
	e.settled(t, buyer, c.ID, photo.ID) // failed: only its sender sees it
	move := service.MoveInput{
		ID: uuid.NewV7(), ConversationID: c.ID, DealID: dealIn(t, e.db, c.ID, buyer, seller), SenderID: buyer, Kind: "offer",
		By: "buyer", Amount: 1_000_000, At: time.Now(),
	}
	if err = e.chat.PostMove(ctx, move); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]uuid.UUID{
		"a message of another chat": foreign.ID,
		"no message":                uuid.NewV7(),
		"a failed photo message":    photo.ID,
		"a deal message":            move.ID,
	} {
		_, sendErr := e.chat.Send(ctx, seller, c.ID, key(), service.SendInput{Text: "ok", ReplyTo: &id})
		var ae *apperr.Error
		if !errors.Is(sendErr, domain.ErrInvalidMessage) || !errors.As(sendErr, &ae) || ae.Fields["reply_to"] == "" {
			t.Errorf("%s: %v", name, sendErr)
		}
	}
}

func TestTheQuoteComesWithTheMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c, buyer, seller := e.conversation(t)
	first := e.messages(t, buyer, c.ID)[0]
	m, err := e.chat.Send(ctx, seller, c.ID, key(), service.SendInput{Text: "Còn 3 tháng", ReplyTo: &first.ID})
	if err != nil || m.ReplyTo == nil || m.ReplyTo.Text != "Còn bảo hành không?" || m.ReplyTo.SenderID != buyer {
		t.Fatalf("%+v, %v", m.ReplyTo, err)
	}
	if got := e.messages(t, buyer, c.ID)[0]; got.ReplyTo == nil || got.ReplyTo.ID != first.ID {
		t.Fatalf("the page carries the quote: %+v", got.ReplyTo)
	}
}

// UC-DEAL-01 E3.
func TestSuspendedPersonWritesToSupport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c, buyer, _ := e.conversation(t)
	help, err := e.chat.OpenSupport(ctx, buyer, uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	accounttest.Suspend(t, e.accounts, buyer)
	if _, err = e.chat.Send(ctx, buyer, help.ID, key(), service.SendInput{Text: "Vì sao tôi bị khoá?"}); err != nil {
		t.Fatalf("support chat: %v", err)
	}
	if _, err = e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "hi"}); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("pair chat: %v", err)
	}
}

func TestSupportChatsRefuseArchiveAndUnread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := e.person(t)
	help, err := e.chat.OpenSupport(ctx, person, uuid.NewV7())
	if err != nil || !help.Other.Official || help.Other.ID != accountdomain.ShopNexusID {
		t.Fatalf("%+v %v", help, err)
	}
	for name, err := range map[string]error{
		"archive":   e.chat.Archive(ctx, person, help.ID),
		"unarchive": e.chat.Unarchive(ctx, person, help.ID),
		"unread":    e.chat.MarkUnread(ctx, person, help.ID),
	} {
		if !errors.Is(err, domain.ErrSupportConversation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestATicketsCardNeedsNoText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	person := e.person(t)
	help, err := e.chat.OpenSupport(ctx, person, uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.chat.Send(ctx, person, help.ID, key(), service.SendInput{OpensTicket: true}); err != nil {
		t.Fatalf("an empty card: %v", err)
	}
	threads, err := e.chat.Threads(ctx, accountdomain.ShopNexusID, []uuid.UUID{help.ID})
	if err != nil || threads[help.ID].Unread != 1 || threads[help.ID].Last == nil {
		t.Fatalf("%+v %v", threads, err)
	}
	if n, err := e.chat.SupportUnread(ctx, accountdomain.ShopNexusID); err != nil || n != 1 {
		t.Fatalf("support unread %d %v", n, err)
	}
}

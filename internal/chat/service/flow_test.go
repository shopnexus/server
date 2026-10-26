package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
)

func TestPhotoMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	m, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Vết này", Photos: []uuid.UUID{e.upload(t, buyer, jpegBytes(t))}})
	if err != nil || m.Status != domain.StatusProcessing {
		t.Fatalf("%+v %v", m, err)
	}
	if len(e.messages(t, seller, c.ID)) != 1 {
		t.Fatal("the other member sees a processing message")
	}
	sent := e.settled(t, buyer, c.ID, m.ID)
	if sent.Status != domain.StatusSent || sent.Photos[0].URL == "" || sent.Photos[0].ThumbURL == "" {
		t.Fatalf("%+v", sent)
	}
	if ms := e.messages(t, seller, c.ID); len(ms) != 2 || ms[0].ID != m.ID {
		t.Fatalf("seller: %+v", ms)
	}
}

func TestFailedPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	m, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Photos: []uuid.UUID{e.upload(t, buyer, []byte("not a jpeg at all"))}})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.settled(t, buyer, c.ID, m.ID); got.Status != domain.StatusFailed || got.Failure == "" {
		t.Fatalf("%+v", got)
	}
	if len(e.messages(t, seller, c.ID)) != 1 {
		t.Fatal("the other member sees a failed message")
	}
}

func TestRefusedAttachLeavesNoMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	theirs := e.upload(t, seller, jpegBytes(t))
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Photos: []uuid.UUID{theirs}}); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("%v", err)
	}
	if ms := e.messages(t, buyer, c.ID); len(ms) != 1 {
		t.Fatalf("a message was left: %+v", ms)
	}
}

func TestRepeatedKeySendsOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, e.listing(t, seller), "Còn không?")
	k := key()
	for range 2 {
		if _, err := e.chat.Send(ctx, buyer, c.ID, k, service.SendInput{Text: "Một lần thôi"}); err != nil {
			t.Fatal(err)
		}
	}
	if ms := e.messages(t, seller, c.ID); len(ms) != 2 {
		t.Fatalf("%d messages", len(ms))
	}
}

// blockAfterOpen is a store whose first OpenPair commits, then the seller
// blocks the buyer and the call fails, as a crash before Restate journals
// the step would.
type blockAfterOpen struct {
	repository.Chats

	accounts      accountsvc.Service
	seller, buyer uuid.UUID
	fired         *atomic.Bool
}

func (b blockAfterOpen) OpenPair(ctx context.Context, c domain.Conversation) (domain.Conversation, error) {
	got, err := b.Chats.OpenPair(ctx, c)
	if err != nil || !b.fired.CompareAndSwap(false, true) {
		return got, err
	}
	if err = b.accounts.Block(ctx, b.seller, b.buyer); err != nil {
		return got, err
	}
	return got, errors.New("lost before the journal")
}

func TestOpenRetriedAfterItsWrite(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)
	deps := e.deps
	deps.Prefix = restatetest.Prefix()
	deps.Chats = blockAfterOpen{Chats: e.deps.Chats, accounts: e.accounts, seller: seller, buyer: buyer, fired: &atomic.Bool{}}
	chat := service.New(deps)
	restatetest.Start(t, chat.Durable()...)
	c, err := chat.Open(context.Background(), buyer, key(), service.OpenInput{ListingID: listing, Text: "Còn không?"})
	if err != nil {
		t.Fatalf("a retry of the conversation's write refused the send it had passed: %v", err)
	}
	if ms := e.messages(t, buyer, c.ID); len(ms) != 1 {
		t.Fatalf("%+v", ms)
	}
}

package worker_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
)

// A kind switched off still writes its notice and sends no push; a kind
// whose switch is on pushes, and messages off pushes no chat message.
func TestSwitchedOffKindListsWithoutPush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	offer := uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{{ID: offer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000}},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	message := chatsvc.MessageFacts{ID: uuid.NewV7(), ConversationID: uuid.NewV7(), RecipientID: f.SellerID, SenderName: "Đức Huy", Text: "Còn không bạn?"}
	e.worker.Deals, e.worker.Chats = deals{facts: f}, chats{facts: message}
	e.start(t)
	off := domain.DefaultSettings()
	off.Offers, off.Messages = false, false
	if err := e.notices.PutSettings(ctx, f.SellerID, off); err != nil {
		t.Fatal(err)
	}
	if err := e.notices.PutToken(ctx, uuid.NewV7(), f.SellerID, "seller-phone"); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{offer.String()}},
	})
	e.deliver(t, "MessageSent", events.Event{ID: message.ID, Kind: events.MessageSent, SubjectID: message.ID, OccurredAt: time.Now()})
	if l, err := e.notices.List(ctx, f.SellerID, page.Request{}); err != nil || len(l.Items) != 1 || l.Items[0].Type != domain.TypeOffer {
		t.Fatalf("the offer is listed: %+v %v", l, err)
	}
	if n := e.push.count("seller-phone"); n != 0 {
		t.Fatalf("offers and messages off pushed %d", n)
	}
	e.deliver(t, "AccountUnsuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: f.SellerID, OccurredAt: time.Now()})
	if n := e.push.count("seller-phone"); n != 1 {
		t.Fatalf("a notice without a switch pushed %d", n)
	}
}

// Every switch off leaves the notices that always push pushing by day.
func TestAlwaysPushIgnoresSwitches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	e.start(t)
	me := accounttest.New(t, e.db)
	if err := e.notices.PutSettings(ctx, me, domain.Settings{Quiet: domain.Quiet{On: true, From: "22:00", To: "07:00"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.notices.PutToken(ctx, uuid.NewV7(), me, "phone"); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "AccountSuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountSuspended, SubjectID: me, OccurredAt: time.Now()})
	e.deliver(t, "AccountUnsuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: me, OccurredAt: time.Now()})
	if n := e.push.count("phone"); n != 2 {
		t.Fatalf("pushes: %d", n)
	}
}

// Inside quiet hours nothing pushes, a notice that always pushes included,
// and the notices are still listed; the same account with quiet hours off
// gets the push.
func TestQuietHoursHoldEveryPush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	e.worker.Now = func() time.Time { return night }
	e.start(t)
	me := accounttest.New(t, e.db)
	if err := e.notices.PutToken(ctx, uuid.NewV7(), me, "phone"); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "AccountSuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountSuspended, SubjectID: me, OccurredAt: time.Now()})
	if l, _ := e.notices.List(ctx, me, page.Request{}); len(l.Items) != 1 {
		t.Fatalf("the notice is listed: %+v", l)
	}
	if n := e.push.count("phone"); n != 0 {
		t.Fatalf("quiet hours pushed %d", n)
	}
	loud := domain.DefaultSettings()
	loud.Quiet.On = false
	if err := e.notices.PutSettings(ctx, me, loud); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "AccountUnsuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: me, OccurredAt: time.Now()})
	if n := e.push.count("phone"); n != 1 {
		t.Fatalf("quiet hours off: %d pushes", n)
	}
}

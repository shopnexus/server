package worker_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// mailEnv is a deal whose buyer asked for COD and made an offer, with the
// seller's email address on file.
type mailEnv struct {
	env

	f          dealsvc.MoveFacts
	cod, offer uuid.UUID
}

const sellerMail = "nam@example.com"

func newMailEnv(t *testing.T) mailEnv {
	t.Helper()
	cod, offer := uuid.NewV7(), uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{
			{ID: cod, Kind: dealdomain.MoveCodRequested, By: dealdomain.Buyer},
			{ID: offer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000},
		},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	e.worker.Deals = deals{facts: f}
	e.worker.Accounts = accounts{emails: map[uuid.UUID]string{f.SellerID: sellerMail}}
	return mailEnv{env: e, f: f, cod: cod, offer: offer}
}

func (e *mailEnv) moved(t *testing.T, moves ...uuid.UUID) events.Event {
	t.Helper()
	ids := make([]any, len(moves))
	for i, m := range moves {
		ids[i] = m.String()
	}
	ev := events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: e.f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": ids},
	}
	e.deliver(t, "DealMoved", ev)
	return ev
}

// An order notice is mailed once, even at night with its push switched
// off; an offer is not mailed, and a buyer without an address gets none.
// UC-SHR-03 A12 · BR-SHR-01.
func TestOrderNoticesAreMailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newMailEnv(t)
	e.worker.Now = func() time.Time { return night }
	e.start(t)
	st := domain.DefaultSettings()
	st.Orders = false
	if err := e.notices.PutSettings(ctx, e.f.SellerID, st); err != nil {
		t.Fatal(err)
	}
	ev := e.moved(t, e.cod, e.offer)
	e.deliver(t, "DealMoved", ev)
	got := e.mail.all()
	if len(got) != 1 || got[0].Subject != "Đức Huy chọn nhận COD Xe đạp Asama" || got[0].Key != ev.ID.String()+"/"+e.f.SellerID.String()+"/0" {
		t.Fatalf("one COD mail: %+v", got)
	}
	e.deliver(t, "AccountSuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountSuspended, SubjectID: e.f.BuyerID, OccurredAt: time.Now()})
	if l, _ := e.notices.List(ctx, e.f.BuyerID, page.Request{}, ""); len(l.Items) != 1 || e.mail.count() != 1 {
		t.Fatalf("an account without an address got a mail: %+v, %d mails", l, e.mail.count())
	}
}

// order_emails off stops order mail only: an account notice still goes.
// UC-SHR-03 A8 · BR-SHR-01.
func TestOrderEmailsOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newMailEnv(t)
	e.start(t)
	st := domain.DefaultSettings()
	st.OrderEmails = false
	if err := e.notices.PutSettings(ctx, e.f.SellerID, st); err != nil {
		t.Fatal(err)
	}
	e.moved(t, e.cod)
	if got := e.mail.all(); len(got) != 0 {
		t.Fatalf("order_emails off mailed %+v", got)
	}
	e.deliver(t, "AccountUnsuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: e.f.SellerID, OccurredAt: time.Now()})
	if got := e.mail.all(); len(got) != 1 || got[0].Subject != "Tài khoản của bạn đã được mở khóa" {
		t.Fatalf("the account notice: %+v", got)
	}
}

// A mail the provider refuses is dropped; the notice stays listed.
// UC-SHR-03 A12.
func TestRefusedMailIsDropped(t *testing.T) {
	t.Parallel()
	e := newMailEnv(t)
	e.mail.refuse = true
	e.start(t)
	e.moved(t, e.cod)
	if l, err := e.notices.List(context.Background(), e.f.SellerID, page.Request{}, ""); err != nil || len(l.Items) != 1 {
		t.Fatalf("the notice: %+v %v", l, err)
	}
}

// A recipient who reads English gets the push and the mail in English.
// UC-SHR-03.
func TestEnglishRecipient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newMailEnv(t)
	e.worker.Accounts = accounts{
		emails: map[uuid.UUID]string{e.f.SellerID: sellerMail}, locales: map[uuid.UUID]locale.Tag{e.f.SellerID: locale.English},
	}
	e.start(t)
	if err := e.notices.PutToken(ctx, uuid.NewV7(), e.f.SellerID, "seller-phone"); err != nil {
		t.Fatal(err)
	}
	e.moved(t, e.cod)
	if e.push.title["seller-phone"] != "Orders" || e.push.body["seller-phone"] != "Đức Huy asked for cash on delivery for Xe đạp Asama" {
		t.Fatalf("push %q %q", e.push.title["seller-phone"], e.push.body["seller-phone"])
	}
	if got := e.mail.all(); len(got) != 1 || got[0].Subject != "Đức Huy chose cash on delivery for Xe đạp Asama" {
		t.Fatalf("mail %+v", got)
	}
}

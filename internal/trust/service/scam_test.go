package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
	"github.com/shopnexus/server/internal/trust/service"
)

// raise writes a warning to victim about suspect in conversation c, as a
// check would.
func raise(t *testing.T, e env, c chatdomain.Conversation, victim, suspect uuid.UUID, codes []domain.Code, at time.Time) {
	t.Helper()
	w, err := e.scams.Get(context.Background(), c.ID, victim)
	if errors.Is(err, repository.ErrNotFound) {
		w, err = domain.NewWarning(c.ID, victim, suspect), nil
	}
	if err != nil {
		t.Fatal(err)
	}
	w.Raise(codes, nil, at, nil)
	ck := domain.Check{
		ID: uuid.NewV7(), ConversationID: c.ID, MessageIDs: []uuid.UUID{uuid.NewV7()},
		Classifier: "test", A: codes, B: []domain.Code{}, CreatedAt: at,
	}
	if err = e.scams.Record(context.Background(), ck, []repository.WarningWrite{{Warning: &w, WriteID: uuid.NewV7()}},
		at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
}

func pairOf(t *testing.T, e env) chatdomain.Conversation {
	t.Helper()
	c, err := chatrepo.NewChats(e.db).OpenPair(context.Background(), chatdomain.NewConversation(uuid.NewV7(), e.person(t), e.person(t), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestScamWarningsView(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c := pairOf(t, e)
	raise(t, e, c, c.AccountB, c.AccountA, []domain.Code{domain.CodeOffApp, domain.CodePrepayment}, time.Now())
	got, err := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID, uuid.NewV7()})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	v := got[c.ID]
	if v.Dismissed || len(v.Signals) != 2 || v.Signals[0].Code != "prepayment_request" ||
		v.Signals[0].Text != "Đòi chuyển tiền hoặc đặt cọc trước" {
		t.Fatalf("%+v", v)
	}
	if mine, _ := e.trust.ScamWarnings(ctx, c.AccountA, []uuid.UUID{c.ID}); len(mine) != 0 {
		t.Fatalf("the suspect sees %+v", mine)
	}
	if none, err := e.trust.ScamWarnings(ctx, c.AccountB, nil); err != nil || len(none) != 0 {
		t.Fatalf("no conversations: %v %v", none, err)
	}
}

func TestDismissScamWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c := pairOf(t, e)
	raise(t, e, c, c.AccountB, c.AccountA, []domain.Code{domain.CodePrepayment}, time.Now().Add(-time.Minute))
	// The warned member blocks the suspect and can still dismiss.
	if err := e.accounts.Block(ctx, c.AccountB, c.AccountA); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := e.trust.DismissScamWarning(ctx, c.AccountB, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}); !got[c.ID].Dismissed {
		t.Fatalf("%+v", got)
	}
	// A new message code brings it back.
	raise(t, e, c, c.AccountB, c.AccountA, []domain.Code{domain.CodeSensitive}, time.Now().Add(time.Minute))
	if got, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}); got[c.ID].Dismissed {
		t.Fatalf("after a new code: %+v", got)
	}
	// The suspect has no warning to dismiss; that is fine.
	if err := e.trust.DismissScamWarning(ctx, c.AccountA, c.ID); err != nil {
		t.Fatalf("without a warning: %v", err)
	}
	// Someone else gets conversation_not_found.
	if err := e.trust.DismissScamWarning(ctx, e.person(t), c.ID); !errors.Is(err, chatdomain.ErrNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
}

func TestDeleteScamWarnings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c := pairOf(t, e)
	raise(t, e, c, c.AccountB, c.AccountA, []domain.Code{domain.CodePrepayment}, time.Now())
	if err := e.trust.DeleteScamWarnings(ctx, c.AccountB); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAccountAge(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	young := e.person(t)
	got, err := service.AccountAge{Accounts: e.accounts}.Signals(context.Background(), young, domain.Window{}, time.Now())
	if err != nil || len(got) != 1 || got[0].Code != domain.CodeNewAccount || got[0].Params["days"] != 1 {
		t.Fatalf("young: %+v %v", got, err)
	}
	got, err = service.AccountAge{Accounts: e.accounts}.Signals(context.Background(), young, domain.Window{}, time.Now().Add(8*24*time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("a week later: %+v %v", got, err)
	}
}

// marketStub answers MarketPrice from a map.
type marketStub struct {
	catalogsvc.Service

	prices map[uuid.UUID]catalogsvc.MarketPrice
	asked  []uuid.UUID
}

func (m *marketStub) MarketPrice(_ context.Context, id uuid.UUID, _, _ float64) (catalogsvc.MarketPrice, bool, error) {
	m.asked = append(m.asked, id)
	p, ok := m.prices[id]
	return p, ok, nil
}

// MarketGap reads the newest listing of the suspect's that the window
// names, once each, and skips the other member's.
func TestMarketGap(t *testing.T) {
	t.Parallel()
	suspect, other := uuid.NewV7(), uuid.NewV7()
	theirs, mine, unknown := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	m := &marketStub{prices: map[uuid.UUID]catalogsvc.MarketPrice{
		theirs: {SellerID: suspect, Price: 5_000_000, Median: 10_000_000, SampleSize: 8},
		mine:   {SellerID: other, Price: 1, Median: 10_000_000, SampleSize: 8},
	}}
	w := domain.Window{Lines: []domain.Line{
		{ListingID: &theirs}, {ListingID: &mine}, {ListingID: &unknown}, {ListingID: &unknown},
	}}
	got, err := service.MarketGap{Catalog: m, TextMin: 0.45, PhotoMin: 0.6}.Signals(context.Background(), suspect, w, time.Now())
	if err != nil || len(got) != 1 || got[0].Code != domain.CodePriceBelow || got[0].Params["percent"] != 50 {
		t.Fatalf("%+v %v", got, err)
	}
	if len(m.asked) != 3 || m.asked[0] != unknown || m.asked[2] != theirs {
		t.Fatalf("asked %v", m.asked)
	}
}

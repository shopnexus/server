package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
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
	got, err := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID, uuid.NewV7()}, locale.Vietnamese)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	v := got[c.ID]
	if v.Dismissed || len(v.Signals) != 2 || v.Signals[0].Code != "prepayment_request" ||
		v.Signals[0].Text != "Đòi chuyển tiền hoặc đặt cọc trước" {
		t.Fatalf("%+v", v)
	}
	if en, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}, locale.English); en[c.ID].Signals[0].Text != "Asks for money or a deposit up front" {
		t.Fatalf("en %+v", en[c.ID])
	}
	if mine, _ := e.trust.ScamWarnings(ctx, c.AccountA, []uuid.UUID{c.ID}, locale.Vietnamese); len(mine) != 0 {
		t.Fatalf("the suspect sees %+v", mine)
	}
	if none, err := e.trust.ScamWarnings(ctx, c.AccountB, nil, locale.Vietnamese); err != nil || len(none) != 0 {
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
	if got, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}, locale.Vietnamese); !got[c.ID].Dismissed {
		t.Fatalf("%+v", got)
	}
	// A new message code brings it back.
	raise(t, e, c, c.AccountB, c.AccountA, []domain.Code{domain.CodeSensitive}, time.Now().Add(time.Minute))
	if got, _ := e.trust.ScamWarnings(ctx, c.AccountB, []uuid.UUID{c.ID}, locale.Vietnamese); got[c.ID].Dismissed {
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

// A deleted account loses its warnings and helpful marks, and its
// unresolved reports are withdrawn without their notes; a resolved one stays.
func TestClearAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	c := pairOf(t, e)
	gone := c.AccountB
	raise(t, e, c, gone, c.AccountA, []domain.Code{domain.CodePrepayment}, time.Now())
	seller, buyer, other := e.person(t), e.person(t), e.person(t)
	report := func(in service.ReportInput) service.ReportView {
		t.Helper()
		v, _, err := e.trust.Report(ctx, gone, in)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	open := report(service.ReportInput{ListingID: catalogtest.Listing(t, e.db, seller), Reason: domain.ReasonScam, Note: "Đòi cọc"})
	withdrawn := report(service.ReportInput{AccountID: seller, Reason: domain.ReasonFake, Note: "Hàng giả"})
	if err := e.trust.WithdrawReport(ctx, gone, withdrawn.ID); err != nil {
		t.Fatal(err)
	}
	resolved := report(service.ReportInput{AccountID: other, Reason: domain.ReasonFake, Note: "Giả mạo"})
	if _, err := e.trust.ResolveReportCase(ctx, other, domain.OutcomeDismissed, e.member(t, "Lan")); err != nil {
		t.Fatal(err)
	}
	_, order := e.completed(t, buyer, seller)
	v, err := e.trust.PutReview(ctx, buyer, order, 5, "Tốt", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.reveal(t, order)
	if err = e.trust.MarkHelpful(ctx, gone, v.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = e.trust.ClearAccount(ctx, gone); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := e.trust.ScamWarnings(ctx, gone, []uuid.UUID{c.ID}, locale.Vietnamese); len(got) != 0 {
		t.Fatalf("warnings: %+v", got)
	}
	mine, err := e.trust.MyReports(ctx, gone, page.Request{})
	if err != nil || len(mine.Items) != 3 {
		t.Fatalf("%+v %v", mine, err)
	}
	for _, r := range mine.Items {
		want, note := domain.ReportWithdrawn, ""
		if r.ID == resolved.ID {
			want, note = domain.ReportResolved, "Giả mạo"
		}
		if r.Status != want || r.Note != note {
			t.Errorf("report %v (open %v): %+v", r.ID, r.ID == open.ID, r)
		}
	}
	got, err := e.trust.Reviews(ctx, nil, seller, domain.RoleSeller, page.Request{})
	if err != nil || len(got.Items) != 1 || got.Items[0].Helpful.Count != 0 {
		t.Fatalf("helpful: %+v %v", got, err)
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

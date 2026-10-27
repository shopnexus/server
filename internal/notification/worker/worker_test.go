package worker_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/notification/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/provider/push"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

type deals struct {
	dealsvc.Service

	facts dealsvc.MoveFacts
}

func (d deals) Moves(_ context.Context, id uuid.UUID, ids []uuid.UUID) (dealsvc.MoveFacts, bool, error) {
	if id != d.facts.DealID {
		return dealsvc.MoveFacts{}, false, nil
	}
	f := d.facts
	f.Moves = nil
	for _, m := range d.facts.Moves {
		for _, want := range ids {
			if m.ID == want {
				f.Moves = append(f.Moves, m)
			}
		}
	}
	return f, true, nil
}

type chats struct {
	chatsvc.Service

	facts chatsvc.MessageFacts
}

func (c chats) MessageFacts(context.Context, uuid.UUID) (chatsvc.MessageFacts, bool, error) {
	return c.facts, true, nil
}

type catalog struct{ catalogsvc.Service }

type trust struct{ trustsvc.Service }

// auth answers that every session but the revoked ones is active.
type auth struct {
	authsvc.Service

	revoked map[uuid.UUID]bool
}

func (a auth) ActiveSessions(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		out[id] = !a.revoked[id]
	}
	return out, nil
}

// sender records every push and answers a set error once per token.
type sender struct {
	mu   sync.Mutex
	sent map[string]int
	body map[string]string
	once map[string]error
}

func (s *sender) Send(_ context.Context, token string, m push.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.once[token]; ok {
		delete(s.once, token)
		return err
	}
	s.sent[token]++
	s.body[token] = m.Body
	return nil
}

func (s *sender) count(token string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[token]
}

type env struct {
	db      *pgxpool.Pool
	notices repository.Notices
	push    *sender
	worker  worker.Worker
	client  *ingress.Client
}

// newEnv builds the worker on a fresh database; a test sets its fakes, then
// calls start.
func newEnv(t *testing.T, revoked map[uuid.UUID]bool, once map[string]error) env {
	t.Helper()
	db := pgtest.New(t)
	notices := repository.NewNotices(db)
	s := &sender{sent: map[string]int{}, body: map[string]string{}, once: once}
	w := worker.Worker{
		Prefix: restatetest.Prefix(), Notices: notices, Deals: deals{}, Chats: chats{}, Catalog: catalog{}, Trust: trust{},
		Auth: auth{revoked: revoked}, Push: s, Log: slog.New(slog.DiscardHandler),
	}
	return env{db: db, notices: notices, push: s, worker: w}
}

func (e *env) start(t *testing.T) {
	t.Helper()
	e.client = restatetest.Start(t, restate.Reflect(e.worker))
}

func (e *env) account(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) deliver(t *testing.T, handler string, ev events.Event) {
	t.Helper()
	// The bus delivers an event once per ID; the same key here attaches a
	// redelivery to the first run, as it does there.
	if _, err := ingress.Service[events.Event, restate.Void](e.client, e.worker.ServiceName(), handler).
		Request(context.Background(), ev, restate.WithIdempotencyKey(ev.ID.String())); err != nil {
		t.Fatal(err)
	}
}

func TestDealMoved(t *testing.T) {
	t.Parallel()
	offer := uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealsvc.MoveFact{{ID: offer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000}},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = e.account(t), e.account(t)
	e.worker.Deals = deals{facts: f}
	e.start(t)
	session := uuid.NewV7()
	if err := e.notices.PutToken(context.Background(), session, f.SellerID, "seller-phone", time.Now()); err != nil {
		t.Fatal(err)
	}
	ev := events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{offer.String(), uuid.NewV7().String()}},
	}
	e.deliver(t, "DealMoved", ev)
	e.deliver(t, "DealMoved", ev)
	rows, err := e.notices.List(context.Background(), f.SellerID, page.Request{})
	if err != nil || len(rows) != 1 || rows[0].Type != "offer" {
		t.Fatalf("one notice: %+v %v", rows, err)
	}
	if n := e.push.count("seller-phone"); n != 1 || e.push.body["seller-phone"] != "Đức Huy trả 1,2 tr cho Xe đạp Asama" {
		t.Fatalf("one push: %d %q", n, e.push.body["seller-phone"])
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{uuid.NewV7().String()}},
	})
	if rows, _ = e.notices.List(context.Background(), f.SellerID, page.Request{}); len(rows) != 1 {
		t.Fatalf("a move the deal lacks tells no one: %+v", rows)
	}
}

func TestMessageSentPushesOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	recipient := e.account(t)
	conv := uuid.NewV7()
	e.worker.Chats = chats{facts: chatsvc.MessageFacts{
		ID: uuid.NewV7(), ConversationID: conv, SenderID: uuid.NewV7(), RecipientID: recipient, SenderName: "Hoàng Nam", Text: "Còn không bạn?",
	}}
	e.start(t)
	if err := e.notices.PutToken(context.Background(), uuid.NewV7(), recipient, "phone", time.Now()); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "MessageSent", events.Event{ID: uuid.NewV7(), Kind: events.MessageSent, SubjectID: uuid.NewV7(), OccurredAt: time.Now()})
	if e.push.count("phone") != 1 || e.push.body["phone"] != "Hoàng Nam: Còn không bạn?" {
		t.Fatalf("push: %d %q", e.push.count("phone"), e.push.body["phone"])
	}
	if rows, _ := e.notices.List(context.Background(), recipient, page.Request{}); len(rows) != 0 {
		t.Fatalf("a message lists nothing: %+v", rows)
	}
}

func TestDeliverDropsEndedAndDeadTokens(t *testing.T) {
	t.Parallel()
	revoked, dead, retried := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	e := newEnv(t, map[uuid.UUID]bool{revoked: true}, map[string]error{"dead": push.ErrDeadToken, "flaky": push.ErrRetry})
	e.start(t)
	me := e.account(t)
	ctx := context.Background()
	for session, token := range map[uuid.UUID]string{revoked: "revoked", dead: "dead", retried: "flaky", uuid.NewV7(): "fine"} {
		if err := e.notices.PutToken(ctx, session, me, token, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	e.deliver(t, "AccountUnsuspended", events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: me, OccurredAt: time.Now()})
	if e.push.count("revoked") != 0 || e.push.count("fine") != 1 || e.push.count("flaky") != 1 {
		t.Fatalf("sends: revoked %d, fine %d, flaky %d", e.push.count("revoked"), e.push.count("fine"), e.push.count("flaky"))
	}
	tokens, err := e.notices.Tokens(ctx, me)
	if err != nil || len(tokens) != 2 {
		t.Fatalf("the revoked and dead tokens are gone: %+v %v", tokens, err)
	}
}

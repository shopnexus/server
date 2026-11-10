package worker_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/notification/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
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

func (c chats) MessageFacts(_ context.Context, id uuid.UUID) (chatsvc.MessageFacts, bool, error) {
	if id != c.facts.ID {
		return chatsvc.MessageFacts{}, false, nil
	}
	return c.facts, true, nil
}

type catalog struct {
	catalogsvc.Service

	listings map[uuid.UUID]catalogsvc.Summary
}

func (c catalog) Summary(_ context.Context, id uuid.UUID) (catalogsvc.Summary, bool, error) {
	s, ok := c.listings[id]
	return s, ok, nil
}

type trust struct{ trustsvc.Service }

// accounts answers that every session but the revoked ones is active, names
// every account Hoàng Nam, and pages followers, the newest follow first.
type accounts struct {
	accountsvc.Service

	revoked   map[uuid.UUID]bool
	followers []accountsvc.Follower // oldest follow first
}

func (accounts) Get(_ context.Context, id uuid.UUID) (accountsvc.AccountView, error) {
	return accountsvc.AccountView{ID: id, DisplayName: "Hoàng Nam"}, nil
}

func (a accounts) Followers(_ context.Context, _ uuid.UUID, p page.Request) (page.Page[accountsvc.Follower], error) {
	var rows []accountsvc.Follower
	for i := len(a.followers) - 1; i >= 0 && len(rows) < p.Fetch(); i-- {
		if f := a.followers[i]; f.ID.String() < p.Before().String() {
			rows = append(rows, f)
		}
	}
	return page.Of(rows, p, func(f accountsvc.Follower) uuid.UUID { return f.ID }), nil
}

func (a accounts) ActiveSessions(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
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
	notices service.Service
	push    *sender
	worker  worker.Worker
	client  *ingress.Client
}

// newEnv builds the worker on a fresh database; a test sets its fakes, then
// calls start.
func newEnv(t *testing.T, revoked map[uuid.UUID]bool, once map[string]error) env {
	t.Helper()
	db := pgtest.New(t)
	notices := service.New(service.Deps{Notices: repository.NewNotices(db), Now: time.Now})
	s := &sender{sent: map[string]int{}, body: map[string]string{}, once: once}
	w := worker.Worker{
		Prefix: restatetest.Prefix(), Notices: notices, Deals: deals{}, Chats: chats{}, Catalog: catalog{}, Trust: trust{},
		Accounts: accounts{revoked: revoked}, Push: s, Log: slog.New(slog.DiscardHandler), FollowerPage: 2,
	}
	return env{db: db, notices: notices, push: s, worker: w}
}

func (e *env) start(t *testing.T) {
	t.Helper()
	e.client = restatetest.Start(t, restate.Reflect(e.worker))
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

// followed makes n follower accounts of a seller and a listing of the
// seller's in the fakes, and returns them.
func (e *env) followed(t *testing.T, n int, status catalogdomain.Status) (seller, listing uuid.UUID, followers []uuid.UUID) {
	t.Helper()
	seller, listing = accounttest.New(t, e.db), uuid.NewV7()
	fs := make([]accountsvc.Follower, 0, n)
	for range n {
		f := accounttest.New(t, e.db)
		followers = append(followers, f)
		fs = append(fs, accountsvc.Follower{ID: uuid.NewV7(), AccountID: f})
	}
	e.worker.Accounts = accounts{followers: fs}
	e.worker.Catalog = catalog{listings: map[uuid.UUID]catalogsvc.Summary{
		listing: {ID: listing, SellerID: seller, Title: "Xe đạp Asama", Status: status},
	}}
	return seller, listing, followers
}

// noticesOf polls account's list until it holds n notices.
func (e *env) noticesOf(t *testing.T, account uuid.UUID, n int) []service.Item {
	t.Helper()
	var items []service.Item
	testenv.Eventually(t, 30*time.Second, func() error {
		l, err := e.notices.List(context.Background(), account, page.Request{})
		if err != nil {
			return err
		}
		if items = l.Items; len(items) != n {
			return fmt.Errorf("%d notices, want %d", len(items), n)
		}
		return nil
	})
	return items
}

// A first publication tells the seller and every follower, across pages of
// two, once even when the event comes again.
func TestListingPublishedTellsFollowers(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	seller, listing, followers := e.followed(t, 5, catalogdomain.StatusActive)
	e.start(t)
	if err := e.notices.PutToken(context.Background(), uuid.NewV7(), followers[0], "follower-phone"); err != nil {
		t.Fatal(err)
	}
	ev := events.Event{
		ID: uuid.NewV7(), Kind: events.ListingPublished, SubjectID: listing, OccurredAt: time.Now(),
		Payload: map[string]any{"first": true},
	}
	e.deliver(t, "ListingPublished", ev)
	e.deliver(t, "ListingPublished", ev)
	for _, f := range followers {
		items := e.noticesOf(t, f, 1)
		if items[0].Type != domain.TypeFollowedListing || items[0].Text != "Hoàng Nam vừa đăng tin mới: Xe đạp Asama" {
			t.Fatalf("follower %s: %+v", f, items[0])
		}
	}
	if items := e.noticesOf(t, seller, 1); items[0].Type != domain.TypeListingLive {
		t.Fatalf("seller: %+v", items[0])
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		if n := e.push.count("follower-phone"); n != 1 {
			return fmt.Errorf("%d pushes", n)
		}
		return nil
	})
}

// A publication that is not the first tells the seller only.
func TestRepublishTellsNoFollower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	seller, listing, followers := e.followed(t, 1, catalogdomain.StatusActive)
	e.start(t)
	e.deliver(t, "ListingPublished", events.Event{ID: uuid.NewV7(), Kind: events.ListingPublished, SubjectID: listing, OccurredAt: time.Now()})
	e.noticesOf(t, seller, 1)
	// ListingPublished sent nothing when the request returned, so nothing can
	// arrive later.
	if l, _ := e.notices.List(context.Background(), followers[0], page.Request{}); len(l.Items) != 0 {
		t.Fatalf("a republish told a follower: %+v", l)
	}
}

// TellFollower run twice for one event and follower, under two idempotency
// keys so Restate runs both, leaves one notice even when the follower read
// it in between, so the collapse key no longer holds it: the store keeps one
// notice per recipient and event. The second run pushes again, as a step
// retried after its notice committed must; the key <event id>/<follower id>
// that ListingPublished sends with is what keeps the push to one.
func TestTellFollowerTwiceWritesOneNotice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	_, listing, followers := e.followed(t, 1, catalogdomain.StatusActive)
	e.start(t)
	if err := e.notices.PutToken(ctx, uuid.NewV7(), followers[0], "follower-phone"); err != nil {
		t.Fatal(err)
	}
	in := worker.FollowerNotice{EventID: uuid.NewV7(), Follower: followers[0], ListingID: listing, SellerName: "Hoàng Nam", At: time.Now()}
	tell := func() {
		t.Helper()
		if _, err := ingress.Service[worker.FollowerNotice, restate.Void](e.client, e.worker.ServiceName(), "TellFollower").
			Request(ctx, in, restate.WithIdempotencyKey(uuid.NewV7().String())); err != nil {
			t.Fatal(err)
		}
	}
	tell()
	if err := e.notices.ReadAll(ctx, followers[0]); err != nil {
		t.Fatal(err)
	}
	tell()
	l, err := e.notices.List(ctx, followers[0], page.Request{})
	if err != nil || len(l.Items) != 1 || l.Unread != 0 || l.Items[0].Type != domain.TypeFollowedListing {
		t.Fatalf("two runs, one notice: %+v %v", l, err)
	}
	if n := e.push.count("follower-phone"); n != 2 {
		t.Fatalf("each run pushes: %d", n)
	}
}

// TellFollower tells no one of a listing gone off sale, and two listings of
// one seller leave one unread notice, of the newer.
func TestTellFollower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	seller, hidden, followers := e.followed(t, 1, catalogdomain.StatusHidden)
	older, newer := uuid.NewV7(), uuid.NewV7()
	listings := e.worker.Catalog.(catalog).listings
	listings[older] = catalogsvc.Summary{ID: older, SellerID: seller, Title: "Xe đạp Asama", Status: catalogdomain.StatusActive}
	listings[newer] = catalogsvc.Summary{ID: newer, SellerID: seller, Title: "Đàn guitar Yamaha", Status: catalogdomain.StatusActive}
	e.start(t)
	tell := func(listing uuid.UUID, at time.Time) {
		t.Helper()
		in := worker.FollowerNotice{EventID: uuid.NewV7(), Follower: followers[0], ListingID: listing, SellerName: "Hoàng Nam", At: at}
		if _, err := ingress.Service[worker.FollowerNotice, restate.Void](e.client, e.worker.ServiceName(), "TellFollower").
			Request(context.Background(), in, restate.WithIdempotencyKey(in.EventID.String())); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now()
	tell(hidden, at)
	if l, _ := e.notices.List(context.Background(), followers[0], page.Request{}); len(l.Items) != 0 {
		t.Fatalf("a hidden listing told: %+v", l)
	}
	tell(older, at.Add(time.Second))
	tell(newer, at.Add(2*time.Second))
	l, err := e.notices.List(context.Background(), followers[0], page.Request{})
	if err != nil || len(l.Items) != 1 || l.Unread != 1 || l.Items[0].Text != "Hoàng Nam vừa đăng tin mới: Đàn guitar Yamaha" {
		t.Fatalf("collapse: %+v %v", l, err)
	}
}

func TestDealMoved(t *testing.T) {
	t.Parallel()
	offer := uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{{ID: offer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000}},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	e.worker.Deals = deals{facts: f}
	e.start(t)
	session := uuid.NewV7()
	if err := e.notices.PutToken(context.Background(), session, f.SellerID, "seller-phone"); err != nil {
		t.Fatal(err)
	}
	ev := events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{offer.String(), uuid.NewV7().String()}},
	}
	e.deliver(t, "DealMoved", ev)
	e.deliver(t, "DealMoved", ev)
	l, err := e.notices.List(context.Background(), f.SellerID, page.Request{})
	if err != nil || len(l.Items) != 1 || l.Items[0].Type != "offer" {
		t.Fatalf("one notice: %+v %v", l, err)
	}
	if n := e.push.count("seller-phone"); n != 1 || e.push.body["seller-phone"] != "Đức Huy trả 1,2 tr cho Xe đạp Asama" {
		t.Fatalf("one push: %d %q", n, e.push.body["seller-phone"])
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{uuid.NewV7().String()}},
	})
	if l, _ = e.notices.List(context.Background(), f.SellerID, page.Request{}); len(l.Items) != 1 {
		t.Fatalf("a move the deal lacks tells no one: %+v", l)
	}
}

// A move delivered after a newer one on the same deal neither replaces the
// newer notice nor pushes, read or unread.
func TestLateMoveTellsNothing(t *testing.T) {
	t.Parallel()
	older, newer, oldest := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{
			{ID: oldest, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_000_000},
			{ID: older, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000},
			{ID: newer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_300_000},
		},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	e.worker.Deals = deals{facts: f}
	e.start(t)
	ctx := context.Background()
	if err := e.notices.PutToken(ctx, uuid.NewV7(), f.SellerID, "seller-phone"); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	move := func(id uuid.UUID, occurred time.Time) {
		e.deliver(t, "DealMoved", events.Event{
			ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: occurred,
			Payload: map[string]any{"moves": []any{id.String()}},
		})
	}
	move(newer, at)
	move(older, at.Add(-time.Minute))
	if n := e.push.count("seller-phone"); n != 1 || e.push.body["seller-phone"] != "Đức Huy trả 1,3 tr cho Xe đạp Asama" {
		t.Fatalf("the late move pushed: %d %q", n, e.push.body["seller-phone"])
	}
	if err := e.notices.ReadAll(ctx, f.SellerID); err != nil {
		t.Fatal(err)
	}
	move(oldest, at.Add(-2*time.Minute))
	l, err := e.notices.List(ctx, f.SellerID, page.Request{})
	if err != nil || len(l.Items) != 1 || l.Unread != 0 || e.push.count("seller-phone") != 1 {
		t.Fatalf("a move older than a read notice: %+v %v, %d pushes", l, err, e.push.count("seller-phone"))
	}
}

func TestMessageSentPushesOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	recipient := accounttest.New(t, e.db)
	message := chatsvc.MessageFacts{
		ID: uuid.NewV7(), ConversationID: uuid.NewV7(), RecipientID: recipient, SenderName: "Hoàng Nam",
		Text: "Còn không bạn? " + strings.Repeat("ạ", 100),
	}
	e.worker.Chats = chats{facts: message}
	e.start(t)
	if err := e.notices.PutToken(context.Background(), uuid.NewV7(), recipient, "phone"); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "MessageSent", events.Event{ID: message.ID, Kind: events.MessageSent, SubjectID: message.ID, OccurredAt: time.Now()})
	want := "Hoàng Nam: Còn không bạn? " + strings.Repeat("ạ", 85)
	if e.push.count("phone") != 1 || e.push.body["phone"] != want {
		t.Fatalf("push: %d %q", e.push.count("phone"), e.push.body["phone"])
	}
	gone := uuid.NewV7()
	e.deliver(t, "MessageSent", events.Event{ID: gone, Kind: events.MessageSent, SubjectID: gone, OccurredAt: time.Now()})
	if e.push.count("phone") != 1 {
		t.Fatalf("a message that is gone pushed: %d", e.push.count("phone"))
	}
	if l, _ := e.notices.List(context.Background(), recipient, page.Request{}); len(l.Items) != 0 {
		t.Fatalf("a message lists nothing: %+v", l)
	}
}

func TestDeliverDropsEndedAndDeadTokens(t *testing.T) {
	t.Parallel()
	revoked, dead, retried := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	e := newEnv(t, map[uuid.UUID]bool{revoked: true}, map[string]error{"dead": push.ErrDeadToken, "flaky": push.ErrRetry})
	e.start(t)
	me := accounttest.New(t, e.db)
	ctx := context.Background()
	for session, token := range map[uuid.UUID]string{revoked: "revoked", dead: "dead", retried: "flaky", uuid.NewV7(): "fine"} {
		if err := e.notices.PutToken(ctx, session, me, token); err != nil {
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

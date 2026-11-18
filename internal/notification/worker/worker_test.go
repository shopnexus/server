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

	"github.com/shopnexus/server/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/notification/worker"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
	"github.com/shopnexus/server/internal/provider/push"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
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

// support has one ticket, whose chat is conversation and whose owner is
// owner.
type support struct {
	supportsvc.Service

	ticket, conversation, owner uuid.UUID
}

func (s support) OfConversation(_ context.Context, account, conversation uuid.UUID) (supportsvc.TicketView, bool, error) {
	if conversation != s.conversation || account != s.owner {
		return supportsvc.TicketView{}, false, nil
	}
	return supportsvc.TicketView{ID: s.ticket, ConversationID: conversation}, true, nil
}

type discovery struct {
	discoverysvc.Service

	watches map[uuid.UUID]discoverysvc.WatchFacts
	drops   map[uuid.UUID]discoverysvc.PriceDropFacts
}

func (d discovery) WatchFacts(_ context.Context, id uuid.UUID) (discoverysvc.WatchFacts, bool, error) {
	f, ok := d.watches[id]
	return f, ok, nil
}

func (d discovery) PriceDropFacts(_ context.Context, id uuid.UUID) (discoverysvc.PriceDropFacts, bool, error) {
	f, ok := d.drops[id]
	return f, ok, nil
}

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
	mu    sync.Mutex
	sent  map[string]int
	body  map[string]string
	title map[string]string
	data  map[string]map[string]string
	tag   map[string]string
	once  map[string]error
}

func (s *sender) Send(_ context.Context, token string, m push.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.once[token]; ok {
		delete(s.once, token)
		return err
	}
	s.sent[token]++
	s.body[token], s.title[token], s.data[token], s.tag[token] = m.Body, m.Title, m.Data, m.Tag
	return nil
}

func (s *sender) count(token string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[token]
}

// noon and night are times on Vietnam's clock outside and inside the
// default quiet hours, 22:00 to 07:00. The worker runs at noon unless a
// test moves it.
var (
	noon  = time.Date(2026, 11, 14, 12, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	night = time.Date(2026, 11, 14, 23, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
)

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
	notices := service.New(service.Deps{Notices: repository.NewNotices(db), Settings: repository.NewSettings(db), Now: time.Now})
	s := &sender{sent: map[string]int{}, body: map[string]string{}, title: map[string]string{}, data: map[string]map[string]string{}, tag: map[string]string{}, once: once}
	w := worker.Worker{
		Prefix: restatetest.Prefix(), Notices: notices, Deals: deals{}, Chats: chats{}, Catalog: catalog{}, Trust: trust{}, Discovery: discovery{}, Support: support{},
		Accounts: accounts{revoked: revoked}, Push: s, Log: slog.New(slog.DiscardHandler), FollowerPage: 2,
		Now: func() time.Time { return noon },
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
	if _, err := e.notices.ReadAll(ctx, followers[0]); err != nil {
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
	offer, raise := uuid.NewV7(), uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{
			{ID: offer, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_200_000},
			{ID: raise, Kind: dealdomain.MoveOffer, By: dealdomain.Buyer, Amount: 1_300_000},
		},
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
	// The deal's notice is still unread, so a second offer rewrites it under
	// its id, and the push replaces the first one in the tray.
	tag := "notification:" + l.Items[0].ID.String()
	if e.push.tag["seller-phone"] != tag {
		t.Fatalf("tagged by the notice: %q, want %q", e.push.tag["seller-phone"], tag)
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{raise.String()}},
	})
	if l, _ = e.notices.List(context.Background(), f.SellerID, page.Request{}); len(l.Items) != 1 || e.push.count("seller-phone") != 2 ||
		e.push.tag["seller-phone"] != tag {
		t.Fatalf("one notice, a second push with its tag: %+v %d %q", l, e.push.count("seller-phone"), e.push.tag["seller-phone"])
	}
}

// The steps of two orders of one deal, the first cancelled and the second
// shipped, tell of their own orders: each notice and its push open its
// order, with that order's reason or shipment, though the deal's latest
// order is the second.
func TestOrderMovesTellTheirOwnOrders(t *testing.T) {
	t.Parallel()
	first, second := uuid.NewV7(), uuid.NewV7()
	cancelled, shipped := uuid.NewV7(), uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{
			{ID: cancelled, Kind: dealdomain.MoveOrderCancelled, By: dealdomain.Seller, OrderID: &first},
			{ID: shipped, Kind: dealdomain.MoveShipped, By: dealdomain.Seller, OrderID: &second},
		},
		Orders: map[uuid.UUID]dealdomain.Order{
			first:  {ID: first, Status: dealdomain.OrderCancelled, CancelReason: dealdomain.CancelBySeller},
			second: {ID: second, Status: dealdomain.OrderShipped, Shipment: &dealdomain.Shipment{Carrier: "ghn", TrackingCode: "8A2K9"}},
		},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	e.worker.Deals = deals{facts: f}
	e.start(t)
	ctx := context.Background()
	if err := e.notices.PutToken(ctx, uuid.NewV7(), f.BuyerID, "buyer-phone"); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: at,
		Payload: map[string]any{"moves": []any{cancelled.String()}},
	})
	if d := e.push.data["buyer-phone"]; d["target_kind"] != "order" || d["target_id"] != first.String() {
		t.Fatalf("the cancellation's push: %+v", d)
	}
	if _, err := e.notices.ReadAll(ctx, f.BuyerID); err != nil {
		t.Fatal(err)
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: at.Add(time.Minute),
		Payload: map[string]any{"moves": []any{shipped.String()}},
	})
	if d := e.push.data["buyer-phone"]; d["target_kind"] != "order" || d["target_id"] != second.String() {
		t.Fatalf("the shipment's push: %+v", d)
	}
	items := e.noticesOf(t, f.BuyerID, 2)
	if items[0].Type != "shipped" || items[0].Target != (domain.Target{Kind: "order", ID: second.String()}) ||
		items[0].Text != "Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9" {
		t.Errorf("the second order's notice: %+v", items[0])
	}
	if items[1].Type != "order_cancelled" || items[1].Target != (domain.Target{Kind: "order", ID: first.String()}) ||
		items[1].Text != "Hoàng Nam huỷ đơn Xe đạp Asama" {
		t.Errorf("the first order's notice: %+v", items[1])
	}
}

// Moves that name no order, as the release before writes them while a
// deploy rolls out, tell of the deal's latest order as before: a shipment
// keeps its carrier and code, a seller's timeout tells the seller too, and
// each notice opens the deal.
func TestMovesWithoutAnOrderTellOfTheLatest(t *testing.T) {
	t.Parallel()
	latest := uuid.NewV7()
	shipped, timedOut := uuid.NewV7(), uuid.NewV7()
	f := dealsvc.MoveFacts{
		DealID: uuid.NewV7(), ListingTitle: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam",
		Moves: []dealdomain.Move{
			{ID: shipped, Kind: dealdomain.MoveShipped, By: dealdomain.Seller},
			{ID: timedOut, Kind: dealdomain.MoveOrderCancelled, By: dealdomain.System},
		},
		Order: &dealdomain.Order{
			ID: latest, Status: dealdomain.OrderCancelled, CancelReason: dealdomain.CancelSellerTimeout,
			Shipment: &dealdomain.Shipment{Carrier: "ghn", TrackingCode: "8A2K9"},
		},
	}
	e := newEnv(t, nil, nil)
	f.BuyerID, f.SellerID = accounttest.New(t, e.db), accounttest.New(t, e.db)
	e.worker.Deals = deals{facts: f}
	e.start(t)
	at := time.Now()
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: at,
		Payload: map[string]any{"moves": []any{shipped.String()}},
	})
	deal := domain.Target{Kind: "deal", ID: f.DealID.String()}
	if n := e.noticesOf(t, f.BuyerID, 1)[0]; n.Text != "Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9" || n.Target != deal {
		t.Fatalf("the shipment: %+v", n)
	}
	e.deliver(t, "DealMoved", events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: f.DealID, OccurredAt: at.Add(time.Minute),
		Payload: map[string]any{"moves": []any{timedOut.String()}},
	})
	if n := e.noticesOf(t, f.SellerID, 1)[0]; n.Text != "Bạn chưa tạo đơn Xe đạp Asama, đơn đã huỷ" || n.Target != deal {
		t.Fatalf("the seller's timeout: %+v", n)
	}
	if n := e.noticesOf(t, f.BuyerID, 1)[0]; n.Text != "Người bán chưa gửi Xe đạp Asama, đơn của bạn đã huỷ" {
		t.Fatalf("the buyer's timeout: %+v", n)
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
	if _, err := e.notices.ReadAll(ctx, f.SellerID); err != nil {
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

// A reply in a ticket's chat pushes to the ticket's owner under ShopNexus's
// name and opens the ticket; the owner's message to ShopNexus pushes nothing.
func TestASupportReplyOpensItsTicket(t *testing.T) {
	t.Parallel()
	for _, toOwner := range []bool{true, false} {
		e := newEnv(t, nil, nil)
		owner, other := accounttest.New(t, e.db), accounttest.New(t, e.db)
		message := chatsvc.MessageFacts{
			ID: uuid.NewV7(), ConversationID: uuid.NewV7(), RecipientID: owner, SenderName: "ShopNexus",
			Text: "Bạn chưa trả tiền nên không mất gì.", Kind: chatdomain.KindSupport,
		}
		if !toOwner {
			message.RecipientID = other
		}
		ticket := uuid.NewV7()
		e.worker.Chats = chats{facts: message}
		e.worker.Support = support{ticket: ticket, conversation: message.ConversationID, owner: owner}
		e.start(t)
		for account, token := range map[uuid.UUID]string{owner: "owner", other: "other"} {
			if err := e.notices.PutToken(context.Background(), uuid.NewV7(), account, token); err != nil {
				t.Fatal(err)
			}
		}
		e.deliver(t, "MessageSent", events.Event{ID: message.ID, Kind: events.MessageSent, SubjectID: message.ID, OccurredAt: time.Now()})
		e.push.mu.Lock()
		if !toOwner {
			if e.push.sent["owner"]+e.push.sent["other"] != 0 {
				t.Errorf("a message to ShopNexus pushed: %v", e.push.sent)
			}
		} else if e.push.title["owner"] != "ShopNexus" || e.push.body["owner"] != "Bạn chưa trả tiền nên không mất gì." ||
			e.push.tag["owner"] != "conversation:"+message.ConversationID.String() || e.push.data["owner"]["type"] != "support_message" ||
			e.push.data["owner"]["target_kind"] != "ticket" || e.push.data["owner"]["target_id"] != ticket.String() {
			t.Errorf("title %q body %q tag %q data %v", e.push.title["owner"], e.push.body["owner"], e.push.tag["owner"], e.push.data["owner"])
		}
		e.push.mu.Unlock()
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

// A deleted account loses its notices, push tokens and settings, and a second
// account.deleted finds nothing left.
func TestAccountDeletedForgetsTheAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	e.start(t)
	ctx := context.Background()
	gone := accounttest.New(t, e.db)
	if _, err := e.notices.Record(ctx, uuid.NewV7(), domain.Suspended(gone, "", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := e.notices.PutToken(ctx, uuid.NewV7(), gone, "phone"); err != nil {
		t.Fatal(err)
	}
	quiet := domain.DefaultSettings()
	quiet.Reviews = false
	if err := e.notices.PutSettings(ctx, gone, quiet); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		id := uuid.NewV7()
		e.deliver(t, "AccountDeleted", events.Event{ID: id, Kind: events.AccountDeleted, SubjectID: gone, OccurredAt: time.Now()})
	}
	if l, _ := e.notices.List(ctx, gone, page.Request{}); len(l.Items) != 0 {
		t.Fatalf("notices left: %+v", l)
	}
	if tokens, _ := e.notices.Tokens(ctx, gone); len(tokens) != 0 {
		t.Fatalf("tokens left: %+v", tokens)
	}
	if st, _ := e.notices.Settings(ctx, gone); st != domain.DefaultSettings() {
		t.Fatalf("settings left: %+v", st)
	}
}

// A watch's sweeps leave one unread notice with the latest count, and a
// price drop tells the saver; facts gone tell no one.
func TestWatchAndPriceNotices(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	me := accounttest.New(t, e.db)
	watch, save, listing := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	e.worker.Discovery = discovery{
		watches: map[uuid.UUID]discoverysvc.WatchFacts{watch: {ID: watch, Account: me, Query: "xe đạp"}},
		drops: map[uuid.UUID]discoverysvc.PriceDropFacts{
			save: {ID: save, Account: me, ListingID: listing, Title: "Xe đạp Asama", SavedPrice: 1_500_000},
		},
	}
	e.start(t)
	at := time.Now()
	e.deliver(t, "SearchWatchMatched", events.Event{
		ID: uuid.NewV7(), Kind: events.SearchWatchMatched, SubjectID: watch,
		Payload: map[string]any{"count": 2}, OccurredAt: at,
	})
	e.deliver(t, "SearchWatchMatched", events.Event{
		ID: uuid.NewV7(), Kind: events.SearchWatchMatched, SubjectID: watch,
		Payload: map[string]any{"count": 5}, OccurredAt: at.Add(time.Minute),
	})
	e.deliver(t, "SearchWatchMatched", events.Event{
		ID: uuid.NewV7(), Kind: events.SearchWatchMatched, SubjectID: uuid.NewV7(),
		Payload: map[string]any{"count": 1}, OccurredAt: at,
	})
	e.deliver(t, "SavedPriceDropped", events.Event{
		ID: uuid.NewV7(), Kind: events.SavedPriceDropped, SubjectID: save,
		Payload: map[string]any{"price": 1_400_000}, OccurredAt: at.Add(2 * time.Minute),
	})
	items := e.noticesOf(t, me, 2)
	if items[0].Type != domain.TypePriceDropped || items[0].Text != "Xe đạp Asama giảm còn 1,4 tr, lúc lưu 1,5 tr" {
		t.Fatalf("%+v", items[0])
	}
	if items[1].Type != domain.TypeWatchMatched || items[1].Text != "Có 5 tin mới cho “xe đạp”" || items[1].Target.Kind != "search_watch" {
		t.Fatalf("%+v", items[1])
	}
}

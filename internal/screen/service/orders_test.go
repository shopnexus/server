package service_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealrepo "github.com/shopnexus/server/internal/deal/repository"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/screen/service"
	"github.com/shopnexus/server/internal/shipping/shippingtest"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustrepo "github.com/shopnexus/server/internal/trust/repository"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// countedDeals counts the order pages read through it.
type countedDeals struct {
	dealsvc.Service

	lists atomic.Int32
}

func (c *countedDeals) ListOrders(ctx context.Context, viewer uuid.UUID, as dealdomain.Party, p page.Request) (page.Page[dealsvc.OrderItem], error) {
	c.lists.Add(1)
	return c.Service.ListOrders(ctx, viewer, as, p)
}

// countedAccounts counts the batch and single account reads through it.
type countedAccounts struct {
	accountsvc.Service

	many, single atomic.Int32
}

func (c *countedAccounts) GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]accountsvc.AccountView, error) {
	c.many.Add(1)
	return c.Service.GetMany(ctx, ids)
}

func (c *countedAccounts) Get(ctx context.Context, id uuid.UUID) (accountsvc.AccountView, error) {
	c.single.Add(1)
	return c.Service.Get(ctx, id)
}

// countedTrust counts the review batches read through it.
type countedTrust struct {
	trustsvc.Service

	reviews atomic.Int32
}

func (c *countedTrust) OrderReviews(ctx context.Context, caller uuid.UUID, orders []uuid.UUID) (map[uuid.UUID]trustsvc.OrderReviews, error) {
	c.reviews.Add(1)
	return c.Service.OrderReviews(ctx, caller, orders)
}

// calls is how many times the orders view called each service since the
// last take.
type calls struct{ lists, many, single, reviews int32 }

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	deals    *dealsvc.Deals
	trust    *trustsvc.Trust
	// now is trust's clock, which a test moves to lock a review.
	now *time.Time

	counted struct {
		deals    *countedDeals
		accounts *countedAccounts
		trust    *countedTrust
	}
	screen *service.Screen
}

// newEnv builds the orders view on real account, deal and trust services
// over one database, each wrapped to count its calls, and serves deal's
// flows so orders can be requested, shipped and cancelled.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, media, in, p)
	chats := chatsvc.New(chatsvc.Deps{
		Chats: chatrepo.NewChats(db), Accounts: accounts, Catalog: c, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	deals := dealsvc.New(dealsvc.Deps{
		Deals: dealrepo.NewDeals(db), Accounts: accounts, Catalog: c, Chats: chats, Shipping: shippingtest.Linked{},
		Waits: dealdomain.OrderWaits, Ingress: in, Prefix: p, Now: time.Now, Log: slog.New(slog.DiscardHandler),
	})
	restatetest.Start(t, slices.Concat(deals.Durable(), media.Durable(), chats.Durable())...)
	e := &env{db: db, accounts: accounts, deals: deals, now: new(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))}
	e.trust = trustsvc.New(trustsvc.Deps{
		Reviews: trustrepo.NewReviews(db), Accounts: accounts, Catalog: c, Deals: deals, Chats: chats,
		Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return *e.now },
	})
	e.counted.deals = &countedDeals{Service: deals}
	e.counted.accounts = &countedAccounts{Service: accounts}
	e.counted.trust = &countedTrust{Service: e.trust}
	e.screen = service.New(service.Deps{Deals: e.counted.deals, Accounts: e.counted.accounts, Trust: e.counted.trust})
	return e
}

// take returns the calls counted since the last take and starts again.
func (e *env) take() calls {
	return calls{
		lists: e.counted.deals.lists.Swap(0), many: e.counted.accounts.many.Swap(0),
		single: e.counted.accounts.single.Swap(0), reviews: e.counted.trust.reviews.Swap(0),
	}
}

// pending has buyer agree a deal on a new listing of seller's and ask for
// COD to the address given, returning the order.
func (e *env) pending(t *testing.T, buyer, seller, address uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	d, _, err := e.deals.Open(ctx, buyer, catalogtest.Listing(t, e.db, seller))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.deals.Accept(ctx, buyer, d.ID, 1_500_000); err != nil {
		t.Fatal(err)
	}
	v, err := e.deals.RequestCod(ctx, buyer, d.ID, address, "")
	if err != nil {
		t.Fatal(err)
	}
	return v.Order.ID
}

// address adds an address to owner's book.
func (e *env) address(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	a, err := e.accounts.CreateAddress(context.Background(), owner, uuid.NewV7(), accountdomain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: accountdomain.AddressHome,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// read reads one page of the orders view and checks it called each
// service a fixed number of times: the page, the people and the reviews,
// each once.
func (e *env) read(t *testing.T, account uuid.UUID, as dealdomain.Party, p page.Request) service.OrdersScreen {
	t.Helper()
	e.take()
	v, err := e.screen.Orders(context.Background(), account, as, p)
	if err != nil {
		t.Fatal(err)
	}
	want := calls{lists: 1, many: 1, reviews: 1}
	if got := e.take(); got != want {
		t.Fatalf("%s's page made %+v calls, want %+v", as, got, want)
	}
	return v
}

func TestOrdersShowEachSideItsRowsWithTheCounterpart(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, other, buyer := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	to := e.address(t, buyer)
	shipped := e.pending(t, buyer, seller, to)
	if _, err := e.deals.CreateShipment(ctx, seller, shipped, uuid.NewV7().String(), dealsvc.ShipmentInput{
		Mode: "linked", Carrier: "ghn", Service: "express", WeightGrams: 2_000, Pickup: "home", PickupAddressID: e.address(t, seller),
	}); err != nil {
		t.Fatal(err)
	}
	cancelled := e.pending(t, buyer, other, to)
	if _, err := e.deals.CancelOrder(ctx, buyer, cancelled, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.ConfirmReceipt(ctx, buyer, shipped, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	review, err := e.trust.PutReview(ctx, buyer, shipped, 5, "Tốt", nil)
	if err != nil {
		t.Fatal(err)
	}

	bought := e.read(t, buyer, dealdomain.Buyer, page.Request{})
	list, err := e.deals.ListOrders(ctx, buyer, dealdomain.Buyer, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(bought.Items) != 2 || bought.NextCursor != nil || bought.Items[0].Order.ID != cancelled || bought.Items[1].Order.ID != shipped {
		t.Fatalf("buyer's page: %+v", bought)
	}
	for i := range bought.Items {
		if !reflect.DeepEqual(bought.Items[i].Order, list.Items[i]) {
			t.Fatalf("row %d differs from the order list:\n%+v\n%+v", i, bought.Items[i].Order, list.Items[i])
		}
	}
	for i, who := range []uuid.UUID{other, seller} {
		a, getErr := e.accounts.Get(ctx, who)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got := bought.Items[i].Counterpart; !reflect.DeepEqual(got, chatsvc.PersonFromAccount(who, a)) {
			t.Fatalf("row %d counterpart %+v, want %s", i, got, who)
		}
	}
	// The buyer reads their own recipient on a cancelled order, and never
	// the seller's pickup.
	if r := bought.Items[0].Order.Recipient; r.Street == "" || r.Phone == "" {
		t.Fatalf("buyer's cancelled recipient: %+v", r)
	}
	if sh := bought.Items[1].Order.Shipment; sh == nil || sh.Pickup != nil {
		t.Fatalf("buyer's shipment: %+v", sh)
	}
	if r := bought.Items[1].OwnReview; r == nil || r.State != "active" || *r.ID != review.ID || *r.Stars != 5 || *r.Text != "Tốt" || r.Locked ||
		!r.ChangeUntil.Equal(e.now.Add(trustdomain.EditWindow)) {
		t.Fatalf("reviewed row: %+v", r)
	}
	if r := bought.Items[0].OwnReview; r == nil || r.State != "none" || r.ID != nil || r.ChangeUntil != nil || r.Locked {
		t.Fatalf("row without a review: %+v", r)
	}

	sold := e.read(t, seller, dealdomain.Seller, page.Request{})
	if len(sold.Items) != 1 || sold.Items[0].Order.ID != shipped || sold.Items[0].Counterpart.ID != buyer ||
		sold.Items[0].OwnReview == nil || sold.Items[0].OwnReview.State != "none" {
		t.Fatalf("seller's page: %+v", sold)
	}
	if sh := sold.Items[0].Order.Shipment; sh == nil || sh.Pickup == nil || sh.Pickup.Street != "12 Lý Thường Kiệt" {
		t.Fatalf("seller's shipment: %+v", sh)
	}
	// The seller of a cancelled order no longer reads where it was going.
	gone := e.read(t, other, dealdomain.Seller, page.Request{})
	if len(gone.Items) != 1 || gone.Items[0].OwnReview == nil || gone.Items[0].OwnReview.State != "none" {
		t.Fatalf("other seller's page: %+v", gone)
	}
	if r := gone.Items[0].Order.Recipient; r.Street != "" || r.Phone != "" || r.Name != "Hân" || r.Commune == "" {
		t.Fatalf("cancelled recipient for the seller: %+v", r)
	}

	// A deleted review keeps its first dates and, past its seven days,
	// reads locked.
	if err = e.trust.DeleteReview(ctx, buyer, shipped); err != nil {
		t.Fatal(err)
	}
	first := *e.now
	*e.now = first.Add(trustdomain.EditWindow + time.Nanosecond)
	r := e.read(t, buyer, dealdomain.Buyer, page.Request{}).Items[1].OwnReview
	if r.State != "deleted" || !r.Locked || r.Stars != nil || r.Text != nil || *r.ID != review.ID || !r.CreatedAt.Equal(first) ||
		!r.ChangeUntil.Equal(first.Add(trustdomain.EditWindow)) {
		t.Fatalf("deleted and locked: %+v", r)
	}
}

func TestOrdersPageNewestFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	to := e.address(t, buyer)
	older, newer := e.pending(t, buyer, seller, to), e.pending(t, buyer, seller, to)
	first := e.read(t, buyer, dealdomain.Buyer, page.Request{Limit: 1})
	if len(first.Items) != 1 || first.Items[0].Order.ID != newer || first.NextCursor == nil || *first.NextCursor != newer {
		t.Fatalf("first page: %+v", first)
	}
	second := e.read(t, buyer, dealdomain.Buyer, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if len(second.Items) != 1 || second.Items[0].Order.ID != older || second.NextCursor != nil {
		t.Fatalf("second page: %+v", second)
	}
	// Each page counts over every order of the side.
	for _, v := range []service.OrdersScreen{first, second} {
		if v.CodOrders == nil || *v.CodOrders != (service.Cap{Used: 2, Max: dealdomain.MaxOpenOrders}) || v.AwaitingShipment != nil {
			t.Fatalf("buyer's counts: %+v %v", v.CodOrders, v.AwaitingShipment)
		}
	}
	if sold := e.read(t, seller, dealdomain.Seller, page.Request{Limit: 1}); sold.AwaitingShipment == nil || *sold.AwaitingShipment != 2 || sold.CodOrders != nil {
		t.Fatalf("seller's counts: %+v %v", sold.CodOrders, sold.AwaitingShipment)
	}
}

func TestOrdersEmptyPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	nobody := accounttest.WithPhone(t, e.db)
	for as, want := range map[dealdomain.Party]string{
		dealdomain.Buyer:  `{"items":[],"cod_orders":{"used":0,"max":3}}`,
		dealdomain.Seller: `{"items":[],"awaiting_shipment":0}`,
	} {
		v := e.read(t, nobody, as, page.Request{})
		raw, err := json.Marshal(v)
		if err != nil || string(raw) != want {
			t.Fatalf("%s's empty page: %s %v", as, raw, err)
		}
	}
}

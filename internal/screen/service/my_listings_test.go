package service_test

import (
	"context"
	"errors"
	"log/slog"
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
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealrepo "github.com/shopnexus/server/internal/deal/repository"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoveryrepo "github.com/shopnexus/server/internal/discovery/repository"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/screen/service"
	"github.com/shopnexus/server/internal/shipping/shippingtest"
	trustrepo "github.com/shopnexus/server/internal/trust/repository"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// countedCatalog counts the pane pages and the on-sale reads through it.
type countedCatalog struct {
	catalogsvc.Service

	pages, onSale atomic.Int32
}

func (c *countedCatalog) ListMine(ctx context.Context, seller uuid.UUID, pane catalogsvc.Pane, p page.Request) (page.Page[catalogsvc.ListingView], error) {
	c.pages.Add(1)
	return c.Service.ListMine(ctx, seller, pane, p)
}

func (c *countedCatalog) OnSale(ctx context.Context, seller uuid.UUID) ([]uuid.UUID, error) {
	c.onSale.Add(1)
	return c.Service.OnSale(ctx, seller)
}

// countedDiscovery counts the stats batches through it, and fails every
// view count while fail is set.
type countedDiscovery struct {
	discoverysvc.Service

	stats atomic.Int32
	fail  atomic.Bool
}

func (c *countedDiscovery) ListingStats(ctx context.Context, ids []uuid.UUID, since time.Time) (map[uuid.UUID]discoverysvc.ListingStats, error) {
	c.stats.Add(1)
	return c.Service.ListingStats(ctx, ids, since)
}

func (c *countedDiscovery) Viewed(ctx context.Context, viewer, listing uuid.UUID) error {
	if c.fail.Load() {
		return errors.New("db down")
	}
	return c.Service.Viewed(ctx, viewer, listing)
}

// activityDeals counts the deal batches through it.
type activityDeals struct {
	dealsvc.Service

	batches atomic.Int32
}

func (c *activityDeals) ListingActivity(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]dealsvc.ListingActivity, error) {
	c.batches.Add(1)
	return c.Service.ListingActivity(ctx, seller, ids)
}

// countedChats counts the asker batches through it.
type countedChats struct {
	chatsvc.Service

	batches atomic.Int32
}

func (c *countedChats) ListingAskers(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]chatsvc.ListingAsker, error) {
	c.batches.Add(1)
	return c.Service.ListingAskers(ctx, seller, ids)
}

// sellCalls is how many times Tin của bạn called each service.
type sellCalls struct{ pages, onSale, stats, deals, chats int32 }

type sellEnv struct {
	db        *pgxpool.Pool
	accounts  accountsvc.Service
	catalog   *catalogsvc.Catalog
	listings  catalogrepo.Listings
	deals     *dealsvc.Deals
	chats     *chatsvc.Chat
	discovery *discoverysvc.Discovery
	// now is the clock of discovery and the screen, which a test moves to
	// leave the week behind; deals and messages take the real time.
	now *atomic.Pointer[time.Time]

	counted struct {
		catalog   *countedCatalog
		discovery *countedDiscovery
		deals     *activityDeals
		chats     *countedChats
	}
	screen *service.Screen
}

// newSellEnv builds Tin của bạn and the listing view on real catalog,
// deal, chat, discovery and trust services over one database, the
// services the views batch through wrapped to count their calls.
func newSellEnv(t *testing.T) *sellEnv {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	e := &sellEnv{db: db, now: &atomic.Pointer[time.Time]{}}
	e.now.Store(new(time.Now()))
	clock := func() time.Time { return *e.now.Load() }
	e.accounts = accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	e.catalog = catalogtest.Service(db, e.accounts, media, in, p)
	e.listings = catalogrepo.NewListings(db)
	e.chats = chatsvc.New(chatsvc.Deps{
		Chats: chatrepo.NewChats(db), Accounts: e.accounts, Catalog: e.catalog, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	e.deals = dealsvc.New(dealsvc.Deps{
		Deals: dealrepo.NewDeals(db), Accounts: e.accounts, Catalog: e.catalog, Chats: e.chats, Shipping: shippingtest.Linked{},
		Waits: dealdomain.OrderWaits, Ingress: in, Prefix: p, Now: time.Now, Log: slog.New(slog.DiscardHandler),
	})
	restatetest.Start(t, slices.Concat(e.deals.Durable(), media.Durable(), e.chats.Durable())...)
	e.discovery = discoverysvc.New(discoverysvc.Deps{
		Saves: discoveryrepo.NewSaves(db), Views: discoveryrepo.NewViews(db), Catalog: e.catalog, Accounts: e.accounts,
		Log: slog.New(slog.DiscardHandler), Now: clock,
	})
	trust := trustsvc.New(trustsvc.Deps{
		Reviews: trustrepo.NewReviews(db), Accounts: e.accounts, Catalog: e.catalog, Deals: e.deals, Chats: e.chats,
		Log: slog.New(slog.DiscardHandler), Now: time.Now,
	})
	e.counted.catalog = &countedCatalog{Service: e.catalog}
	e.counted.discovery = &countedDiscovery{Service: e.discovery}
	e.counted.deals = &activityDeals{Service: e.deals}
	e.counted.chats = &countedChats{Service: e.chats}
	e.screen = service.New(service.Deps{
		Accounts: e.accounts, Catalog: e.counted.catalog, Deals: e.counted.deals, Chats: e.counted.chats,
		Discovery: e.counted.discovery, Trust: trust, Now: clock, Log: slog.New(slog.DiscardHandler),
	})
	return e
}

// take returns the calls counted since the last take and starts again.
func (e *sellEnv) take() sellCalls {
	return sellCalls{
		pages: e.counted.catalog.pages.Swap(0), onSale: e.counted.catalog.onSale.Swap(0), stats: e.counted.discovery.stats.Swap(0),
		deals: e.counted.deals.batches.Swap(0), chats: e.counted.chats.batches.Swap(0),
	}
}

// read reads one page of Tin của bạn and checks it called each service
// once, whatever the page holds, and catalog's OnSale onSale times.
func (e *sellEnv) read(t *testing.T, seller uuid.UUID, pane catalogsvc.Pane, p page.Request, onSale int32) service.MyListingsScreen {
	t.Helper()
	e.take()
	v, err := e.screen.MyListings(context.Background(), seller, pane, p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := e.take(), (sellCalls{pages: 1, onSale: onSale, stats: 1, deals: 1, chats: 1}); got != want {
		t.Fatalf("%s page made %+v calls, want %+v", pane, got, want)
	}
	return v
}

// open reads the listing view of id as viewer, nil for signed out.
func (e *sellEnv) open(t *testing.T, viewer *uuid.UUID, id uuid.UUID) {
	t.Helper()
	if _, err := e.screen.Listing(context.Background(), viewer, service.ListingRead{Ref: id.String()}); err != nil {
		t.Fatal(err)
	}
}

// address adds an address to owner's book.
func (e *sellEnv) address(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	a, err := e.accounts.CreateAddress(context.Background(), owner, uuid.NewV7(), accountdomain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: accountdomain.AddressHome,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// pending has buyer agree a deal on listing and ask for COD.
func (e *sellEnv) pending(t *testing.T, buyer, listing uuid.UUID) dealsvc.DealView {
	t.Helper()
	ctx := context.Background()
	d, _, err := e.deals.Open(ctx, buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.deals.Accept(ctx, buyer, d.ID, 1_500_000); err != nil {
		t.Fatal(err)
	}
	v, err := e.deals.RequestCod(ctx, buyer, d.ID, e.address(t, buyer), "")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// unpublished inserts a listing of seller's that never left processing, or
// that processing rejected.
func (e *sellEnv) unpublished(t *testing.T, seller uuid.UUID, rejected bool) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.ListingInput{
		Title: "Ghế đôn gỗ", Price: 350_000, CategoryCode: "home.other", Condition: catalogdomain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{photo},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rejected {
		l.Rejected(map[uuid.UUID]string{photo: "not a photo"}, time.Now())
	}
	if err = e.listings.Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l.ID()
}

// Tin của bạn lists every listing on sale with its views, askers, saves
// and open order, and the week sums the listings on sale. A view counts a
// signed-in reader other than the seller once a day; an asker is a person
// with a deal or a message about the listing, once.
func TestMyListingsCountsEachListing(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	ctx := context.Background()
	seller, ann, bao, cara, shipper := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db),
		accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	sold, hidden := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	reserved, deleted := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	processing, rejected := e.unpublished(t, seller, false), e.unpublished(t, seller, true)
	busy := catalogtest.Listing(t, e.db, seller)
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	if err := e.catalog.Delete(ctx, seller, deleted); err != nil {
		t.Fatal(err)
	}
	shipped := e.pending(t, shipper, reserved)
	if _, err := e.deals.CreateShipment(ctx, seller, shipped.Order.ID, uuid.NewV7().String(), dealsvc.ShipmentInput{
		Mode: "linked", Carrier: "ghn", Service: "express", WeightGrams: 2_000, Pickup: "home", PickupAddressID: e.address(t, seller),
	}); err != nil {
		t.Fatal(err)
	}

	// busy: Ann opens it twice and asks for COD, Bao opens it and writes
	// about it, and so does Ann; the seller and a guest open it too. Cara
	// saves it.
	e.open(t, &ann, busy)
	e.open(t, &ann, busy)
	e.open(t, &bao, busy)
	e.open(t, &seller, busy)
	e.open(t, nil, busy)
	order := e.pending(t, ann, busy)
	for _, who := range []uuid.UUID{bao, ann} {
		if _, err := e.chats.Open(ctx, who, uuid.NewV7().String(), chatsvc.OpenInput{ListingID: busy, Text: "Còn không?"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.discovery.Save(ctx, cara, busy); err != nil {
		t.Fatal(err)
	}

	v := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0)
	ids := make([]uuid.UUID, 0, len(v.Items))
	for _, it := range v.Items {
		ids = append(ids, it.Listing.ID)
	}
	if want := []uuid.UUID{busy, rejected, processing, reserved, hidden}; !slices.Equal(ids, want) || v.NextCursor != nil {
		t.Fatalf("on sale: %v, want %v", ids, want)
	}
	statuses := []catalogdomain.Status{
		catalogdomain.StatusActive, catalogdomain.StatusRejected, catalogdomain.StatusProcessing,
		catalogdomain.StatusReserved, catalogdomain.StatusHidden,
	}
	for i, it := range v.Items {
		if it.Listing.Status != statuses[i] {
			t.Fatalf("%s is %s, want %s", it.Listing.ID, it.Listing.Status, statuses[i])
		}
	}
	got := v.Items[0]
	if got.Views != 2 || got.Views7d != 2 || got.Askers != 2 || got.Saves != 1 {
		t.Fatalf("busy: %+v", got)
	}
	if o := got.Order; o == nil || *o != (dealsvc.ListingOrderView{DealID: order.ID, OrderID: order.Order.ID, Status: dealdomain.OrderPending}) {
		t.Fatalf("busy's order: %+v", o)
	}
	if o := v.Items[3].Order; o == nil || o.OrderID != shipped.Order.ID || o.Status != dealdomain.OrderShipped || v.Items[3].Askers != 1 {
		t.Fatalf("reserved: %+v", v.Items[3])
	}
	for _, it := range []service.MyListingsItem{v.Items[1], v.Items[2], v.Items[4]} {
		if it.Views != 0 || it.Askers != 0 || it.Saves != 0 || it.Order != nil {
			t.Fatalf("a quiet listing: %+v", it)
		}
	}
	// The shipper asked about the reserved listing too.
	if v.Week != (service.ListingWeek{Views: 2, Askers: 3, Saves: 1}) {
		t.Fatalf("week: %+v", v.Week)
	}

	// Past the week, the totals stay and the week empties.
	e.now.Store(new(time.Now().Add(8 * 24 * time.Hour)))
	later := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0)
	if it := later.Items[0]; it.Views != 2 || it.Views7d != 0 || it.Askers != 2 || it.Saves != 1 {
		t.Fatalf("busy past the week: %+v", it)
	}
	if later.Week != (service.ListingWeek{}) {
		t.Fatalf("week past the week: %+v", later.Week)
	}
	// Ann opens it again on a later day and counts again.
	e.open(t, &ann, busy)
	if it := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0).Items[0]; it.Views != 3 || it.Views7d != 1 {
		t.Fatalf("busy on a later day: %+v", it)
	}
}

// The sold pane pages by listing ID, newest first, with each sale's time;
// the week still sums the listings on sale, and every page costs the same
// calls.
func TestMyListingsSoldPanePages(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	ctx := context.Background()
	seller, ann := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	sold := make([]uuid.UUID, 0, 3)
	for range 3 {
		id := catalogtest.Listing(t, e.db, seller)
		if _, err := e.catalog.MarkSold(ctx, seller, id); err != nil {
			t.Fatal(err)
		}
		sold = append(sold, id)
	}
	onSale := catalogtest.Listing(t, e.db, seller)
	e.open(t, &ann, onSale)

	first := e.read(t, seller, catalogsvc.PaneSold, page.Request{Limit: 2}, 1)
	if len(first.Items) != 2 || first.Items[0].Listing.ID != sold[2] || first.Items[1].Listing.ID != sold[1] ||
		first.NextCursor == nil || *first.NextCursor != sold[1] {
		t.Fatalf("first page: %+v", first)
	}
	for _, it := range first.Items {
		if it.Listing.Status != catalogdomain.StatusSold || it.Listing.SoldAt == nil {
			t.Fatalf("a sold item: %+v", it.Listing)
		}
	}
	second := e.read(t, seller, catalogsvc.PaneSold, page.Request{Cursor: *first.NextCursor, Limit: 2}, 1)
	if len(second.Items) != 1 || second.Items[0].Listing.ID != sold[0] || second.NextCursor != nil {
		t.Fatalf("second page: %+v", second)
	}
	for _, v := range []service.MyListingsScreen{first, second} {
		if v.Week != (service.ListingWeek{Views: 1}) {
			t.Fatalf("week on the sold pane: %+v", v.Week)
		}
	}
	empty := e.read(t, accounttest.WithPhone(t, e.db), catalogsvc.PaneSold, page.Request{}, 1)
	if len(empty.Items) != 0 || empty.Items == nil || empty.NextCursor != nil || empty.Week != (service.ListingWeek{}) {
		t.Fatalf("a seller with nothing: %+v", empty)
	}
}

// The on-sale pane in pages still sums every listing on sale into the week,
// reading their IDs from catalog; a page that holds them all does not.
func TestMyListingsOnSalePages(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	seller, ann := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	oldest := catalogtest.Listing(t, e.db, seller)
	middle, newest := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	for _, id := range []uuid.UUID{oldest, middle, newest} {
		e.open(t, &ann, id)
	}
	first := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{Limit: 2}, 1)
	if len(first.Items) != 2 || first.Items[0].Listing.ID != newest || first.Items[1].Listing.ID != middle ||
		first.NextCursor == nil || first.Week.Views != 3 {
		t.Fatalf("first page: %+v", first)
	}
	second := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{Cursor: *first.NextCursor, Limit: 2}, 1)
	if len(second.Items) != 1 || second.Items[0].Listing.ID != oldest || second.NextCursor != nil || second.Week.Views != 3 {
		t.Fatalf("second page: %+v", second)
	}
	if whole := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{Limit: 3}, 0); len(whole.Items) != 3 || whole.Week.Views != 3 {
		t.Fatalf("one page of all: %+v", whole)
	}
}

// A failed view count leaves the listing view as it is.
func TestListingViewSurvivesAFailedCount(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	seller, ann := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
	e.counted.discovery.fail.Store(true)
	v, err := e.screen.Listing(context.Background(), &ann, service.ListingRead{Ref: listing.String()})
	if err != nil || v.Viewer == nil || v.Listing.ID != listing {
		t.Fatalf("%+v %v", v, err)
	}
	e.counted.discovery.fail.Store(false)
	if it := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0).Items[0]; it.Views != 0 {
		t.Fatalf("an uncounted view counts: %+v", it)
	}
}

// A prefetch reads the whole view and counts no view; the open the browser
// reports afterwards counts it.
func TestListingPrefetchCountsNoView(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	seller, ann := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
	v, err := e.screen.Listing(context.Background(), &ann, service.ListingRead{Ref: listing.String(), Prefetch: true})
	if err != nil || v.Viewer == nil || v.Listing.ID != listing {
		t.Fatalf("%+v %v", v, err)
	}
	if it := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0).Items[0]; it.Views != 0 {
		t.Fatalf("a prefetch counts: %+v", it)
	}
	if err = e.discovery.Viewed(context.Background(), ann, listing); err != nil {
		t.Fatal(err)
	}
	if it := e.read(t, seller, catalogsvc.PaneOnSale, page.Request{}, 0).Items[0]; it.Views != 1 {
		t.Fatalf("the reported open: %+v", it)
	}
}

// The listing view and the seller view say whether the seller has a phone
// identity.
func TestSellerPhoneVerified(t *testing.T) {
	t.Parallel()
	e := newSellEnv(t)
	for _, c := range []struct {
		seller uuid.UUID
		want   bool
	}{{accounttest.WithPhone(t, e.db), true}, {accounttest.New(t, e.db), false}} {
		listing := catalogtest.Listing(t, e.db, c.seller)
		v, err := e.screen.Listing(context.Background(), nil, service.ListingRead{Ref: listing.String()})
		if err != nil || v.Seller.PhoneVerified != c.want {
			t.Fatalf("listing view: %+v %v, want %v", v.Seller, err, c.want)
		}
		p, err := e.screen.Seller(context.Background(), nil, c.seller, catalogdomain.Near{})
		if err != nil || p.Seller.PhoneVerified != c.want {
			t.Fatalf("seller view: %+v %v, want %v", p.Seller, err, c.want)
		}
	}
}

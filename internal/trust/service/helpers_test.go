package service_test

import (
	"context"
	"log/slog"
	"slices"
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
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/shipping/shippingtest"
	"github.com/shopnexus/server/internal/trust/repository"
	"github.com/shopnexus/server/internal/trust/service"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	deals    *dealsvc.Deals
	chats    *chatsvc.Chat
	media    *mediasvc.Media
	scams    repository.ScamWarnings
	trust    *service.Trust
}

// newEnv builds trust and the modules it calls on a database, and serves
// deal's flows so orders can be shipped.
func newEnv(t *testing.T) env {
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
		Waits: dealdomain.OrderWaits, PollEvery: 2 * time.Hour, Ingress: in, Prefix: p, Now: time.Now, Log: slog.New(slog.DiscardHandler),
	})
	restatetest.Start(t, slices.Concat(deals.Durable(), media.Durable(), chats.Durable())...)
	scams := repository.NewScamWarnings(db)
	trust := service.New(service.Deps{
		Reviews: repository.NewReviews(db), Reports: repository.NewReports(db), Cases: repository.NewCases(db), Scams: scams, Accounts: accounts, Catalog: c,
		Deals: deals, Chats: chats, Media: media, Log: slog.New(slog.DiscardHandler), Now: time.Now,
	})
	return env{db: db, accounts: accounts, deals: deals, chats: chats, media: media, scams: scams, trust: trust}
}

// person creates an account with a verified phone number.
func (e env) person(t *testing.T) uuid.UUID {
	t.Helper()
	return accounttest.WithPhone(t, e.db)
}

// pending puts a listing of seller's on sale and has buyer ask for COD on
// it at the listed price, returning the deal and its order.
func (e env) pending(t *testing.T, buyer, seller uuid.UUID) (deal, order uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	d, _, err := e.deals.Open(ctx, buyer, catalogtest.Listing(t, e.db, seller))
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.accounts.CreateAddress(ctx, buyer, uuid.NewV7(), accountdomain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: accountdomain.AddressHome,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.deals.RequestCod(ctx, buyer, d.ID, 1_500_000, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return d.ID, v.Order.ID
}

// shipped is a pending order the seller shipped under SPX1 with the fake
// carrier.
func (e env) shipped(t *testing.T, buyer, seller uuid.UUID) (deal, order uuid.UUID) {
	t.Helper()
	deal, order = e.pending(t, buyer, seller)
	if _, err := e.deals.CreateShipment(context.Background(), seller, order, uuid.NewV7().String(), dealsvc.ShipmentInput{
		Mode: "manual", Carrier: "spx", TrackingCode: "SPX1",
	}); err != nil {
		t.Fatal(err)
	}
	return deal, order
}

// completed is a shipped order the buyer confirmed receiving.
func (e env) completed(t *testing.T, buyer, seller uuid.UUID) (deal, order uuid.UUID) {
	t.Helper()
	deal, order = e.shipped(t, buyer, seller)
	if _, err := e.deals.ConfirmReceipt(context.Background(), buyer, order, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	return deal, order
}

// reveal ends the blind window of the order's reviews.
func (e env) reveal(t *testing.T, order uuid.UUID) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), `UPDATE reviews SET visible_at = now() - interval '1 second' WHERE order_id = $1`, order); err != nil {
		t.Fatal(err)
	}
}

// conversation has buyer open a conversation on a listing of seller's.
func (e env) conversation(t *testing.T, buyer, seller uuid.UUID) uuid.UUID {
	t.Helper()
	v, err := e.chats.Open(context.Background(), buyer, uuid.NewV7().String(), chatsvc.OpenInput{
		ListingID: catalogtest.Listing(t, e.db, seller), Text: "Còn không bạn?",
	})
	if err != nil {
		t.Fatal(err)
	}
	return v.ID
}

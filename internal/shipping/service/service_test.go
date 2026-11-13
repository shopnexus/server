package service_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/provider/carrier"
	"github.com/shopnexus/server2/internal/shipping/domain"
	"github.com/shopnexus/server2/internal/shipping/repository"
	"github.com/shopnexus/server2/internal/shipping/service"
)

var errNoOrder = errors.New("deal: no order has the code")

// deals plays module deal: open shipments are the set given, and every
// reported code is recorded.
type deals struct {
	open     map[string]bool
	reported []string
}

func (d *deals) funcs() service.Deals {
	return service.Deals{
		ShipmentUpdated: func(_ context.Context, code, status string) error {
			if code == "UNKNOWN" {
				return errNoOrder
			}
			d.reported = append(d.reported, code+" "+status)
			return nil
		},
		OpenShipmentsAt: func(_ context.Context, _ uuid.UUID, c string) (bool, error) { return d.open[c], nil },
		QuoteFacts: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (service.QuoteFacts, error) {
			return service.QuoteFacts{Recipient: carrier.Place{Name: "Hân", Commune: "Phường Bến Nghé", Province: "TP. Hồ Chí Minh"}, Price: 1_500_000}, nil
		},
		NotFound: errNoOrder,
	}
}

type env struct {
	svc   *service.Shipping
	rows  repository.Accounts
	deals *deals
	key   []byte
	owner uuid.UUID
}

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func newEnv(t *testing.T, clients map[carrier.Carrier]carrier.Client) env {
	t.Helper()
	db := pgtest.New(t)
	e := env{rows: repository.NewAccounts(db), deals: &deals{open: map[string]bool{}}, key: newKey(t), owner: accounttest.New(t, db)}
	e.svc = service.New(service.Deps{Accounts: e.rows, Clients: clients, Key: e.key, Deals: e.deals.funcs(), Now: time.Now})
	return e
}

func fakes() map[carrier.Carrier]carrier.Client {
	return map[carrier.Carrier]carrier.Client{carrier.GHTK: carrier.Fake{Carrier: carrier.GHTK}, carrier.GHN: carrier.Fake{Carrier: carrier.GHN}}
}

func TestLinkStoresASealedTokenAndShowsTheShopCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	v, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "an@shop.vn", Password: "x"})
	if err != nil || v.Carrier != "ghtk" || !strings.HasPrefix(v.ShopCode, "S") {
		t.Fatalf("%+v %v", v, err)
	}
	row, err := e.rows.Get(ctx, e.owner, "ghtk")
	if err != nil || strings.Contains(string(row.Token), "fake-ghtk-an@shop.vn") {
		t.Fatalf("stored %q %v", row.Token, err)
	}
	token, err := domain.Open(e.key, row.Token, row.Nonce)
	if err != nil || token != "fake-ghtk-an@shop.vn" {
		t.Fatalf("opened %q %v", token, err)
	}
	if _, err = e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.Accounts(ctx, e.owner)
	if err != nil || len(list) != 2 || list[0].Carrier != "ghn" || list[0].ShopCode != "885" || list[1].Carrier != "ghtk" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestAWrongPasswordIsLoginFailed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, fakes())
	_, err := e.svc.Link(context.Background(), e.owner, "ghtk", carrier.Credentials{Email: "an@shop.vn", Password: "wrong"})
	if !errors.Is(err, domain.ErrLoginFailed) {
		t.Fatalf("%v", err)
	}
	if list, _ := e.svc.Accounts(context.Background(), e.owner); len(list) != 0 {
		t.Fatalf("linked %+v", list)
	}
}

func TestUnlinkCountsOnlyThatCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	for c, cr := range map[string]carrier.Credentials{"ghtk": {Email: "a@b.vn", Password: "x"}, "ghn": {Token: "t", ShopID: "885"}} {
		if _, err := e.svc.Link(ctx, e.owner, c, cr); err != nil {
			t.Fatal(err)
		}
	}
	e.deals.open["ghn"] = true
	if err := e.svc.Unlink(ctx, e.owner, "ghn"); !errors.Is(err, domain.ErrShipmentsOpen) {
		t.Fatalf("ghn with an open shipment: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "ghtk"); err != nil {
		t.Fatalf("ghtk: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "ghtk"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("ghtk again: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "spx"); !errors.Is(err, domain.ErrInvalidCarrier) {
		t.Fatalf("spx: %v", err)
	}
}

func TestNotLinkedAndNoCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("lookup: %v", err)
	}
	// A carrier no seller can link is one this seller has not linked.
	if _, err := e.svc.Lookup(ctx, e.owner, "spx", "SPX1"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("lookup at spx: %v", err)
	}
	if _, err := e.svc.Pickups(ctx, e.owner, "ghn"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("pickups: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "spx", carrier.Credentials{}); !errors.Is(err, domain.ErrInvalidCarrier) {
		t.Fatalf("spx: %v", err)
	}
	none := newEnv(t, map[carrier.Carrier]carrier.Client{})
	for name, err := range map[string]error{
		"link":   func() error { _, err := none.svc.Link(ctx, none.owner, "spx", carrier.Credentials{}); return err }(),
		"lookup": func() error { _, err := none.svc.Lookup(ctx, none.owner, carrier.GHTK, "S1"); return err }(),
		"create": func() error {
			_, err := none.svc.Create(ctx, none.owner, carrier.GHN, carrier.CreateInput{})
			return err
		}(),
		"pickups": func() error { _, err := none.svc.Pickups(ctx, none.owner, "ghtk"); return err }(),
		"quote": func() error {
			_, err := none.svc.Quote(ctx, none.owner, "ghtk", service.QuoteRequest{Service: "x"})
			return err
		}(),
	} {
		if !errors.Is(err, domain.ErrUnavailable) {
			t.Errorf("%s with no carrier: %v", name, err)
		}
	}
}

func TestQuoteChecksTheShipmentThenAsksTheCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	req := service.QuoteRequest{OrderID: uuid.NewV7(), Service: "standard", WeightGrams: 800, PickupPointID: "88256"}
	if _, err := e.svc.Quote(ctx, e.owner, "ghtk", req); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("not linked: %v", err)
	}
	// Someone else's order is not found before the carrier is asked about.
	theirs := newEnv(t, fakes())
	theirs.svc = service.New(service.Deps{Accounts: theirs.rows, Clients: fakes(), Key: theirs.key, Now: time.Now, Deals: service.Deals{
		QuoteFacts: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (service.QuoteFacts, error) {
			return service.QuoteFacts{}, errNoOrder
		},
	}})
	if _, err := theirs.svc.Quote(ctx, theirs.owner, "ghtk", req); !errors.Is(err, errNoOrder) {
		t.Fatalf("someone else's order: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	if fee, err := e.svc.Quote(ctx, e.owner, "ghtk", req); err != nil || fee != 22_000 {
		t.Fatalf("%d %v", fee, err)
	}
	for name, bad := range map[string]service.QuoteRequest{
		"a service":         {OrderID: req.OrderID, Service: "rocket", WeightGrams: 800},
		"no weight":         {OrderID: req.OrderID, Service: "standard"},
		"a point and an id": {OrderID: req.OrderID, Service: "standard", WeightGrams: 800, PickupPointID: "1", AddressID: uuid.NewV7()},
	} {
		if _, err := e.svc.Quote(ctx, e.owner, "ghtk", bad); !errors.Is(err, domain.ErrInvalidShipment) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestReportedMovesTheOrderAndIgnoresUnknownCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if err := e.svc.Reported(ctx, carrier.GHTK, "S1.A1", carrier.StatusDelivered); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Reported(ctx, carrier.GHTK, "UNKNOWN", carrier.StatusDelivered); err != nil {
		t.Fatalf("an unknown code: %v", err)
	}
	if len(e.deals.reported) != 1 || e.deals.reported[0] != "S1.A1 delivered" {
		t.Fatalf("%v", e.deals.reported)
	}
}

func TestAccountDeletedDropsTheTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AccountDeleted(ctx, e.owner); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.svc.Accounts(ctx, e.owner); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}

func TestAWrongKeyTokenIsAnErrorWithoutTheToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	sealed, nonce, err := domain.Seal(newKey(t), "token-of-another-stack")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.rows.Put(ctx, repository.Row{AccountID: e.owner, Carrier: "ghtk", ShopCode: "S1", Token: sealed, Nonce: nonce, LinkedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1.A1")
	if !errors.Is(err, domain.ErrSealedToken) || strings.Contains(err.Error(), "token-of-another-stack") {
		t.Fatalf("%v", err)
	}
	// Linking again replaces the token that does not open.
	if _, err = e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1.A1"); err != nil {
		t.Fatalf("after relinking: %v", err)
	}
}

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/app"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// A stack without a carrier, as production runs, answers 503
// carrier_unavailable on every carrier call; a shipment checks its order
// first. The second server has a database of its own, so its event bus
// drains no one else's events; the shared server's access token holds
// there, since both sign with the same keys.
func TestNoCarrier(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	db := pgtest.New(t)
	cfg.DatabaseURL, cfg.Restate = db.Config().ConnString(), restatetest.Config(t)
	cfg.Carrier.Mode = "none"
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast), fakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	me, _ := newAccount(t)
	auth, order := bearer(me.AccessToken), uuid.NewV7().String()
	check(t, "linking", callAt(t, srv.URL, "PUT", "/v1/carrier-accounts/ghtk", map[string]any{"email": "a@b.vn", "password": "x"}, auth),
		503, "carrier_unavailable")
	check(t, "texting a code", callAt(t, srv.URL, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{"phone": "+84912345678"}, auth),
		503, "carrier_unavailable")
	check(t, "the pickup points", callAt(t, srv.URL, "GET", "/v1/carrier-accounts/ghn/pickups", nil, auth), 503, "carrier_unavailable")
	check(t, "a quote", callAt(t, srv.URL, "GET", "/v1/shipments/quote?order_id="+order+"&carrier=ghtk&service=standard&weight_grams=800",
		nil, auth), 503, "carrier_unavailable")
	check(t, "the webhook", callAt(t, srv.URL, "POST", "/v1/carriers/ghtk/webhook/dev", map[string]any{"label_id": "S1", "status_id": 5}),
		404, "not_found")
	check(t, "a shipment of an unknown order", callAt(t, srv.URL, "POST", "/v1/orders/"+order+"/shipment", linkedBody, auth, newKey()),
		404, "order_not_found")

	// A pending order in the second database: its people and listing are
	// written there, and their tokens signed with the shared keys.
	seller, buyer := accounttest.WithPhone(t, db), accounttest.WithPhone(t, db)
	listing := catalogtest.Listing(t, db, seller)
	as := func(id uuid.UUID) [2]string {
		t.Helper()
		now := time.Now()
		s, _ := accountdomain.NewSession(uuid.NewV7(), id, "e2e", now, now)
		tok, _, signErr := accountdomain.NewSigner(serverConfig.Auth).Sign(s, now)
		if signErr != nil {
			t.Fatal(signErr)
		}
		return bearer(tok)
	}
	address := expect[addressView](t, callAt(t, srv.URL, "POST", "/v1/addresses", addressBody(), as(buyer), newKey()), http.StatusCreated)
	d := expect[dealView](t, callAt(t, srv.URL, "POST", "/v1/deals", map[string]any{"listing_id": listing.String()}, as(buyer)), http.StatusCreated)
	d = expect[dealView](t, callAt(t, srv.URL, "POST", "/v1/deals/"+d.ID+"/cod", codBody(1_500_000, address.ID), as(buyer), newKey()), http.StatusOK)
	check(t, "a shipment", callAt(t, srv.URL, "POST", "/v1/orders/"+d.Order.ID+"/shipment", linkedBody, as(seller), newKey()),
		503, "carrier_unavailable")
}

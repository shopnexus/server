package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/app"
	consoledomain "github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// A stack without a carrier, as production runs, answers 503
// carrier_unavailable on the lookup and on both shipment endpoints, for any
// order. The second server has a database of its own, so its event bus
// drains no one else's events; the shared server's access token holds there,
// since both sign with the same keys.
func TestNoCarrier(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	cfg.DatabaseURL, cfg.Restate, cfg.Carrier = pgtest.New(t).Config().ConnString(), restatetest.Config(t), "none"
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	me, _ := newAccount(t)
	auth, order := bearer(me.AccessToken), uuid.NewV7().String()
	check(t, "a lookup", callAt(t, srv.URL, "GET", "/v1/shipments/lookup?carrier=spx&code=SPX1", nil, auth),
		503, "carrier_unavailable")
	check(t, "a shipment", callAt(t, srv.URL, "POST", "/v1/orders/"+order+"/shipment", shipmentBody("SPX1"), auth, newKey()),
		503, "carrier_unavailable")
	check(t, "a correction", callAt(t, srv.URL, "PUT", "/v1/orders/"+order+"/shipment",
		map[string]any{"carrier": "spx", "tracking_code": "SPX1"}, auth), 503, "carrier_unavailable")
}

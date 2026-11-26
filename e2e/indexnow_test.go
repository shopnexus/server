package e2e

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/app"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// indexNowKey is the shared server's IndexNow key.
const indexNowKey = "e2e0123456789abcdef0123456789abc"

// A server without an IndexNow key serves none.
func TestIndexNowOff(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	cfg.DatabaseURL, cfg.Restate = pgtest.New(t).Config().ConnString(), restatetest.Config(t)
	cfg.IndexNowKey, cfg.Gateway.URL = "", ownGateway(t)
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast), fakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	check(t, "the key", callAt(t, srv.URL, "GET", "/v1/sitemap/indexnow", nil), 404, "indexnow_off")
}

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/app"
	"github.com/shopnexus/server/internal/console"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// monitoringPaths are the monitoring operations, each with a query that
// runs.
var monitoringPaths = []string{
	"/v1/console/monitoring/health",
	"/v1/console/monitoring/metrics?match=http_server",
	"/v1/console/monitoring/metrics/query?query=up",
	"/v1/console/monitoring/logs?query=" + url.QueryEscape(`{service_name="server"}`),
}

// The system's health with its week before, the metrics there are, and
// a query of each kind; a log line comes back masked. UC-MOD-13.
func TestMonitoring(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)

	health := expect[struct {
		Window string `json:"window"`
		API    struct {
			P95Seconds struct {
				Now        *float64 `json:"now"`
				WeekBefore *float64 `json:"week_before"`
			} `json:"p95_seconds"`
		} `json:"api"`
		SlowRoutes []struct {
			Route string `json:"route"`
		} `json:"slow_routes"`
		Visitors struct {
			Top []struct {
				Visitor string  `json:"visitor"`
				Share   float64 `json:"share"`
			} `json:"top"`
		} `json:"visitors"`
		Missing []string `json:"missing"`
	}](t, consoleCall(t, admin, "GET", "/v1/console/monitoring/health?window=1h", nil), http.StatusOK)
	if health.Window != "1h" || *health.API.P95Seconds.Now != 0.5 || *health.API.P95Seconds.WeekBefore != 0.5 || len(health.Missing) != 0 {
		t.Errorf("health %+v", health)
	}
	if len(health.SlowRoutes) != 1 || health.SlowRoutes[0].Route != "/v1/search" || len(health.Visitors.Top) != 2 ||
		health.Visitors.Top[0].Visitor != "g:4f1c2d3e4a5b" || health.Visitors.Top[0].Share != 0.9 {
		t.Errorf("routes %+v visitors %+v", health.SlowRoutes, health.Visitors)
	}

	// A1. The metrics whose name holds match, a histogram once under the
	// name its series share.
	list := expect[struct {
		Metrics []struct {
			Name   string   `json:"name"`
			Type   string   `json:"type"`
			Labels []string `json:"labels"`
		} `json:"metrics"`
	}](t, consoleCall(t, admin, "GET", "/v1/console/monitoring/metrics?match=http_server", nil), http.StatusOK)
	if len(list.Metrics) != 1 || list.Metrics[0].Name != "http_server_request_duration_seconds" || list.Metrics[0].Type != "histogram" ||
		strings.Join(list.Metrics[0].Labels, ",") != "http_route,job" {
		t.Errorf("metrics %+v", list)
	}

	type series struct {
		Labels map[string]string `json:"labels"`
		Points []struct {
			Value *float64 `json:"value"`
		} `json:"points"`
	}
	instant := expect[struct {
		Step   *string  `json:"step"`
		Series []series `json:"series"`
	}](t, consoleCall(t, admin, "GET", "/v1/console/monitoring/metrics/query?query=up", nil), http.StatusOK)
	if instant.Step != nil || len(instant.Series) != 1 || *instant.Series[0].Points[0].Value != 0.5 {
		t.Errorf("an instant %+v", instant)
	}
	ranged := expect[struct {
		Step   *string  `json:"step"`
		Series []series `json:"series"`
	}](t, consoleCall(t, admin, "GET", "/v1/console/monitoring/metrics/query?query=up&range=6h&step=10s", nil), http.StatusOK)
	if ranged.Step == nil || *ranged.Step != "87s" || len(ranged.Series[0].Points) != 2 || ranged.Series[0].Points[1].Value != nil {
		t.Errorf("a range %+v", ranged)
	}

	logs := expect[struct {
		Lines []struct {
			Line   string            `json:"line"`
			Fields map[string]string `json:"fields"`
		} `json:"lines"`
		Truncated bool `json:"truncated"`
	}](t, consoleCall(t, admin, "GET", "/v1/console/monitoring/logs?limit=1&query="+url.QueryEscape(`{service_name="server"}`), nil), http.StatusOK)
	if len(logs.Lines) != 1 || logs.Lines[0].Line != "mail to <email> failed" || logs.Lines[0].Fields["route"] != "/v1/me" || !logs.Truncated {
		t.Errorf("logs %+v", logs)
	}
	if _, noisy := logs.Lines[0].Fields["flags"]; noisy {
		t.Errorf("fields %v", logs.Lines[0].Fields)
	}
}

// What the spec refuses, what the source refuses, a source that is down,
// and a member who is not an admin. UC-MOD-13 E1, E2, E3.
func TestMonitoringRefused(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)
	mod := signInMember(t, consoledomain.RoleModerator)
	get := func(path string) Response { return consoleCall(t, admin, "GET", path, nil) }

	check(t, "a window not offered", get("/v1/console/monitoring/health?window=7m"), 400, "invalid_request")
	check(t, "a match with a space", get("/v1/console/monitoring/metrics?match=a%20b"), 400, "invalid_request")
	check(t, "no query", get("/v1/console/monitoring/metrics/query"), 400, "invalid_request")
	check(t, "a range in weeks", get("/v1/console/monitoring/logs?query=x&range=1w"), 400, "invalid_request")

	// E1. A query the source cannot parse, or over a range past a day.
	check(t, "a metrics query that does not parse", get("/v1/console/monitoring/metrics/query?query=bad"), 422, "monitoring_query_invalid")
	check(t, "two days of metrics", get("/v1/console/monitoring/metrics/query?query=up&range=2d"), 422, "monitoring_query_invalid")
	check(t, "a log query that does not parse", get("/v1/console/monitoring/logs?query=bad"), 422, "monitoring_query_invalid")

	// E2. The source answers 500.
	check(t, "metrics down", get("/v1/console/monitoring/metrics/query?query=down"), 503, "monitoring_unavailable")
	check(t, "logs down", get("/v1/console/monitoring/logs?query=down"), 503, "monitoring_unavailable")

	// E3. Only an admin reads monitoring.
	for _, p := range monitoringPaths {
		check(t, "a moderator: "+p, consoleCall(t, mod, "GET", p, nil), 403, "console_forbidden")
		check(t, "no session: "+p, consoleCall(t, consoleSession{}, "GET", p, nil), 401, "")
	}
}

// A stack without Prometheus and Loki answers monitoring_unavailable on
// every monitoring call. The second server has a database and an admin
// of its own. UC-MOD-13 E2.
func TestMonitoringOff(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	pool := pgtest.New(t)
	cfg.DatabaseURL, cfg.Restate, cfg.Gateway.URL = pool.Config().ConnString(), restatetest.Config(t), ownGateway(t)
	cfg.Monitoring.MetricsURL, cfg.Monitoring.LogsURL = "", ""
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast), fakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	keys, err := consolesvc.NewKeys(cfg.Console, consoledomain.Fast)
	if err != nil {
		t.Fatal(err)
	}
	admin := signInMemberAt(t, srv.URL, console.NewService(pool, console.Settings{Keys: keys, Config: cfg.Console}, nil, time.Now),
		consoledomain.RoleAdmin)
	for _, p := range monitoringPaths {
		check(t, p, callAt(t, srv.URL, "GET", p, nil, [2]string{"X-Requested-With", "shopnexus-console"},
			[2]string{"Cookie", "console_session=" + admin}), 503, "monitoring_unavailable")
	}
}

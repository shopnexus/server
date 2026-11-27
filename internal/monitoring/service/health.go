package service

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// Health is the system's health over a window.
type Health struct {
	Window          string            `json:"window"`
	At              time.Time         `json:"at"`
	API             *API              `json:"api"`
	SlowRoutes      []Route           `json:"slow_routes"`
	RateLimitedPerS Figure            `json:"rate_limited_per_s"`
	Database        *Database         `json:"database"`
	Outbound        []Outbound        `json:"outbound"`
	Restate         []Handler         `json:"restate"`
	Instances       []Instance        `json:"instances"`
	Visitors        *Visitors         `json:"visitors"`
	Queries         map[string]string `json:"queries"`
	Missing         []string          `json:"missing"`
}

// Figure is a figure over the window and over the same window a week
// before.
type Figure struct {
	Now        *float64 `json:"now"`
	WeekBefore *float64 `json:"week_before"`
}

type API struct {
	RequestsPerS Figure `json:"requests_per_s"`
	ErrorRatio   Figure `json:"error_ratio"`
	P95Seconds   Figure `json:"p95_seconds"`
}

type Route struct {
	Route        string   `json:"route"`
	RequestsPerS float64  `json:"requests_per_s"`
	ErrorRatio   float64  `json:"error_ratio"`
	P95Seconds   *float64 `json:"p95_seconds"`
}

type Database struct {
	ConnectionsInUse Figure   `json:"connections_in_use"`
	ConnectionsMax   *float64 `json:"connections_max"`
	WaitSecondsPerS  Figure   `json:"wait_seconds_per_s"`
}

type Outbound struct {
	Host         string   `json:"host"`
	RequestsPerS float64  `json:"requests_per_s"`
	ErrorRatio   float64  `json:"error_ratio"`
	P95Seconds   *float64 `json:"p95_seconds"`
}

type Handler struct {
	Handler         string   `json:"handler"`
	InvocationsPerS float64  `json:"invocations_per_s"`
	P95Seconds      *float64 `json:"p95_seconds"`
}

type Instance struct {
	Instance         string   `json:"instance"`
	CPUCores         *float64 `json:"cpu_cores"`
	MemoryBytes      *float64 `json:"memory_bytes"`
	MemoryLimitBytes *float64 `json:"memory_limit_bytes"`
}

type Visitors struct {
	Requests int64     `json:"requests"`
	Top      []Visitor `json:"top"`
}

type Visitor struct {
	Visitor  string  `json:"visitor"`
	Client   string  `json:"client"`
	Country  string  `json:"country"`
	Requests int64   `json:"requests"`
	Share    float64 `json:"share"`
}

// healthCalls is how many queries one Health runs at a time.
const healthCalls = 8

// The parts of the health and the queries behind them. W is the window.
// Probes and long-lived streams are out of the API's figures: a stream
// stays open for minutes and would make every p95 its own length.
const (
	apiSel      = `http_route!~".*/events|/v1/ws|/healthz|/readyz"`
	qRPS        = `sum(rate(http_server_request_duration_seconds_count{` + apiSel + `}[W]))`
	qErrors     = `(sum(rate(http_server_request_duration_seconds_count{` + apiSel + `,http_response_status_code=~"5.."}[W])) or vector(0)) / sum(rate(http_server_request_duration_seconds_count{` + apiSel + `}[W]))`
	qP95        = `histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{` + apiSel + `}[W])))`
	qRouteP95   = `topk(5, histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket{` + apiSel + `}[W]))) and on (http_route) (sum by (http_route) (increase(http_server_request_duration_seconds_count{` + apiSel + `}[W])) >= 5))`
	qRouteRPS   = `sum by (http_route) (rate(http_server_request_duration_seconds_count{` + apiSel + `}[W]))`
	qRouteErr   = `sum by (http_route) (rate(http_server_request_duration_seconds_count{` + apiSel + `,http_response_status_code=~"5.."}[W]))`
	qLimited    = `sum(rate(http_server_rate_limited_total[W])) or vector(0)`
	qDBInUse    = `max_over_time(sum(db_client_connection_acquired)[W:])`
	qDBMax      = `sum(db_client_connection_max)`
	qDBWait     = `sum(rate(db_client_connection_wait_time_seconds_total[W]))`
	outSel      = `server_address!~"lgtm.*"`
	qOutP95     = `topk(5, histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_request_duration_seconds_bucket{` + outSel + `}[W]))))`
	qOutRPS     = `sum by (server_address) (rate(http_client_request_duration_seconds_count{` + outSel + `}[W]))`
	qOutErr     = `sum by (server_address) (rate(http_client_request_duration_seconds_count{` + outSel + `,error_type=~".+"}[W]) or rate(http_client_request_duration_seconds_count{` + outSel + `,http_response_status_code=~"5.."}[W]))`
	qRestateP95 = `topk(5, histogram_quantile(0.95, sum by (le, restate_service, restate_handler) (rate(restate_invocation_duration_seconds_bucket[W]))) and on (restate_service, restate_handler) (sum by (restate_service, restate_handler) (increase(restate_invocation_duration_seconds_count[W])) >= 5))`
	qRestateRPS = `sum by (restate_service, restate_handler) (rate(restate_invocation_duration_seconds_count[W]))`
	qCPU        = `sum by (service_instance_id) (rate(process_cpu_time_seconds_total[W]))`
	qMemory     = `sum by (service_instance_id) (container_memory_usage_bytes)`
	qMemLimit   = `sum by (service_instance_id) (container_memory_limit_bytes)`
	qVisitors   = `sum(count_over_time({service_name="server"} | visitor != "" [W]))`
	qTop        = `topk(5, sum by (visitor, client, country) (count_over_time({service_name="server"} | visitor != "" [W])))`
)

// health gathers the parts of one Health: each query's answer, and the
// parts a source failed.
type health struct {
	m      *monitoring
	window string
	end    time.Time

	mu      sync.Mutex
	missing map[string]bool
	down    int // parts whose source is down
	parts   int
}

// q puts the window in query, at each [W] and [W:] (a subquery).
func (h *health) q(query string) string {
	return strings.NewReplacer("[W]", "["+h.window+"]", "[W:", "["+h.window+":").Replace(query)
}

// fail notes that part could not be read, and whether its source is down.
func (h *health) fail(part string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.missing[part] = true
	if errors.Is(err, domain.ErrUnavailable) {
		h.down++
	}
}

func (h *health) metric(ctx context.Context, part, query string, at time.Time) []domain.Series {
	h.mu.Lock()
	h.parts++
	h.mu.Unlock()
	s, err := h.m.d.Metrics.Query(ctx, h.q(query), domain.Span{End: at})
	if err != nil {
		h.fail(part, err)
		return nil
	}
	return s
}

func (h *health) scalar(ctx context.Context, part, query string, at time.Time) *float64 {
	s := h.metric(ctx, part, query, at)
	if len(s) == 0 {
		return nil
	}
	return s[0].Last()
}

// figure reads query now and a week before.
func (h *health) figure(ctx context.Context, g *errgroup.Group, part, query string, out *Figure) {
	g.Go(func() error { out.Now = h.scalar(ctx, part, query, h.end); return nil })
	g.Go(func() error { out.WeekBefore = h.scalar(ctx, part, query, h.end.Add(-domain.WeekBefore)); return nil })
}

// by reads query now into out, each series by its label key. Only its
// own call writes out.
func (h *health) by(ctx context.Context, g *errgroup.Group, part, query string, key func(map[string]string) string, out map[string]*float64) {
	g.Go(func() error {
		for _, s := range h.metric(ctx, part, query, h.end) {
			out[key(s.Labels)] = s.Last()
		}
		return nil
	})
}

func (h *health) logs(ctx context.Context, part, query string) []domain.Series {
	h.mu.Lock()
	h.parts++
	h.mu.Unlock()
	got, err := h.m.d.Logs.Query(ctx, h.q(query), domain.Span{End: h.end}, 1)
	if err != nil {
		h.fail(part, err)
		return nil
	}
	return got.Series
}

func label(name string) func(map[string]string) string {
	return func(l map[string]string) string { return l[name] }
}

func handlerOf(l map[string]string) string { return l["restate_service"] + "/" + l["restate_handler"] }

func zero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func ratio(part, whole *float64) float64 {
	if part == nil || whole == nil || *whole == 0 {
		return 0
	}
	return *part / *whole
}

// Health is UC-MOD-13 step 2.
func (m *monitoring) Health(ctx context.Context, window time.Duration) (Health, error) {
	end := m.d.Now().UTC().Truncate(time.Second)
	h := &health{m: m, window: domain.FormatDuration(window), end: end, missing: map[string]bool{}}
	out := Health{
		Window: h.window, At: end, API: &API{}, Database: &Database{},
		SlowRoutes: []Route{}, Outbound: []Outbound{}, Restate: []Handler{}, Instances: []Instance{}, Missing: []string{},
		Queries: map[string]string{
			"api.requests_per_s": h.q(qRPS), "api.error_ratio": h.q(qErrors), "api.p95_seconds": h.q(qP95),
			"slow_routes": h.q(qRouteP95), "rate_limited_per_s": h.q(qLimited),
			"database.connections_in_use": h.q(qDBInUse), "database.connections_max": h.q(qDBMax), "database.wait_seconds_per_s": h.q(qDBWait),
			"outbound": h.q(qOutP95), "restate": h.q(qRestateP95),
			"instances.cpu_cores": h.q(qCPU), "instances.memory_bytes": h.q(qMemory), "instances.memory_limit_bytes": h.q(qMemLimit),
			"visitors": h.q(qTop),
		},
	}
	// 2. Every part at once, each figure beside the week before.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(healthCalls)
	h.figure(gctx, g, "api", qRPS, &out.API.RequestsPerS)
	h.figure(gctx, g, "api", qErrors, &out.API.ErrorRatio)
	h.figure(gctx, g, "api", qP95, &out.API.P95Seconds)
	h.figure(gctx, g, "rate_limited_per_s", qLimited, &out.RateLimitedPerS)
	h.figure(gctx, g, "database", qDBInUse, &out.Database.ConnectionsInUse)
	h.figure(gctx, g, "database", qDBWait, &out.Database.WaitSecondsPerS)
	g.Go(func() error { out.Database.ConnectionsMax = h.scalar(gctx, "database", qDBMax, end); return nil })
	routeP95 := map[string]*float64{}
	routeRPS := map[string]*float64{}
	routeErr := map[string]*float64{}
	outP95 := map[string]*float64{}
	outRPS := map[string]*float64{}
	outErr := map[string]*float64{}
	restP95 := map[string]*float64{}
	restRPS := map[string]*float64{}
	cpu := map[string]*float64{}
	mem := map[string]*float64{}
	memLimit := map[string]*float64{}
	h.by(gctx, g, "slow_routes", qRouteP95, label("http_route"), routeP95)
	h.by(gctx, g, "slow_routes", qRouteRPS, label("http_route"), routeRPS)
	h.by(gctx, g, "slow_routes", qRouteErr, label("http_route"), routeErr)
	h.by(gctx, g, "outbound", qOutP95, label("server_address"), outP95)
	h.by(gctx, g, "outbound", qOutRPS, label("server_address"), outRPS)
	h.by(gctx, g, "outbound", qOutErr, label("server_address"), outErr)
	h.by(gctx, g, "restate", qRestateP95, handlerOf, restP95)
	h.by(gctx, g, "restate", qRestateRPS, handlerOf, restRPS)
	h.by(gctx, g, "instances", qCPU, label("service_instance_id"), cpu)
	h.by(gctx, g, "instances", qMemory, label("service_instance_id"), mem)
	h.by(gctx, g, "instances", qMemLimit, label("service_instance_id"), memLimit)
	var total, top []domain.Series
	g.Go(func() error { total = h.logs(gctx, "visitors", qVisitors); return nil })
	g.Go(func() error { top = h.logs(gctx, "visitors", qTop); return nil })
	_ = g.Wait()

	// A2. With every source down there is no health; with some, the parts
	// that came back.
	if h.down == h.parts {
		return Health{}, domain.ErrUnavailable
	}
	for route, p95 := range routeP95 {
		out.SlowRoutes = append(out.SlowRoutes, Route{Route: route, RequestsPerS: zero(routeRPS[route]), ErrorRatio: ratio(routeErr[route], routeRPS[route]), P95Seconds: p95})
	}
	slices.SortFunc(out.SlowRoutes, func(a, b Route) int { return cmp.Compare(zero(b.P95Seconds), zero(a.P95Seconds)) })
	for host, p95 := range outP95 {
		out.Outbound = append(out.Outbound, Outbound{Host: host, RequestsPerS: zero(outRPS[host]), ErrorRatio: ratio(outErr[host], outRPS[host]), P95Seconds: p95})
	}
	slices.SortFunc(out.Outbound, func(a, b Outbound) int { return cmp.Compare(zero(b.P95Seconds), zero(a.P95Seconds)) })
	for handler, p95 := range restP95 {
		out.Restate = append(out.Restate, Handler{Handler: handler, InvocationsPerS: zero(restRPS[handler]), P95Seconds: p95})
	}
	slices.SortFunc(out.Restate, func(a, b Handler) int { return cmp.Compare(zero(b.P95Seconds), zero(a.P95Seconds)) })
	for id := range mem {
		out.Instances = append(out.Instances, Instance{Instance: id, CPUCores: cpu[id], MemoryBytes: mem[id], MemoryLimitBytes: memLimit[id]})
	}
	slices.SortFunc(out.Instances, func(a, b Instance) int { return strings.Compare(a.Instance, b.Instance) })
	if len(out.Instances) > 20 {
		out.Instances = out.Instances[:20]
	}
	if !h.missing["visitors"] {
		v := &Visitors{Top: []Visitor{}}
		if len(total) > 0 {
			v.Requests = int64(math.Round(zero(total[0].Last())))
		}
		for _, s := range top {
			n := int64(math.Round(zero(s.Last())))
			share := 0.0
			if v.Requests > 0 {
				share = float64(n) / float64(v.Requests)
			}
			v.Top = append(v.Top, Visitor{Visitor: s.Labels["visitor"], Client: s.Labels["client"], Country: s.Labels["country"], Requests: n, Share: share})
		}
		slices.SortFunc(v.Top, func(a, b Visitor) int { return cmp.Compare(b.Requests, a.Requests) })
		out.Visitors = v
	}
	if h.missing["api"] {
		out.API = nil
	}
	if h.missing["database"] {
		out.Database = nil
	}
	for part := range h.missing {
		out.Missing = append(out.Missing, part)
	}
	slices.Sort(out.Missing)
	return out, nil
}

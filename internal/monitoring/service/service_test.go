package service_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
	"github.com/shopnexus/server/internal/monitoring/repository"
	"github.com/shopnexus/server/internal/monitoring/service"
)

var now = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

// answer is what metrics gives a query that holds key.
type answer struct {
	key string
	f   func(at time.Time) []domain.Series
}

// metrics answers a query by the first answer whose key the query holds,
// and keeps every query it ran.
type metrics struct {
	answers []answer
	down    bool

	mu  sync.Mutex
	ran []string
}

func (m *metrics) Query(_ context.Context, q string, span domain.Span) ([]domain.Series, error) {
	m.mu.Lock()
	m.ran = append(m.ran, q)
	m.mu.Unlock()
	if m.down {
		return nil, domain.ErrUnavailable
	}
	if strings.Contains(q, "bad") {
		return nil, domain.Refused("parse error")
	}
	for _, a := range m.answers {
		if strings.Contains(q, a.key) {
			return a.f(span.End), nil
		}
	}
	return nil, nil
}

func (*metrics) Names(context.Context) ([]string, error) {
	return []string{
		"db_client_connection_max", "http_server_request_duration_seconds_bucket", "http_server_request_duration_seconds_count",
		"http_server_request_duration_seconds_sum", "process_cpu_time_seconds_total", "traces_spanmetrics_calls_total", "up",
	}, nil
}

func (*metrics) Metadata(context.Context) (map[string]repository.Meta, error) {
	return map[string]repository.Meta{"http_server_request_duration_seconds": {Type: "histogram", Help: "Duration of HTTP server requests."}}, nil
}

func (*metrics) Labels(_ context.Context, metric string) ([]string, error) {
	switch {
	case strings.HasSuffix(metric, "_bucket"):
		return []string{"http_route", "job", "le"}, nil
	case strings.HasPrefix(metric, "traces_"):
		return []string{"span_name"}, nil
	default:
		return []string{"job"}, nil
	}
}

type logs struct {
	got  repository.Logged
	down bool
}

func (l *logs) Query(context.Context, string, domain.Span, int) (repository.Logged, error) {
	if l.down {
		return repository.Logged{}, domain.ErrUnavailable
	}
	return l.got, nil
}

func one(labels map[string]string, value float64) []domain.Series {
	return []domain.Series{{Labels: labels, Points: []domain.Point{{At: now, Value: new(value)}}}}
}

// week answers now, or then a week before.
func week(now, then float64) func(time.Time) []domain.Series {
	return func(at time.Time) []domain.Series {
		if at.Before(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)) {
			return one(nil, then)
		}
		return one(nil, now)
	}
}

func healthy() *metrics {
	return &metrics{answers: []answer{
		{"histogram_quantile(0.95, sum by (le, http_route)", func(time.Time) []domain.Series {
			return append(one(map[string]string{"http_route": "/v1/search"}, 2.4), one(map[string]string{"http_route": "/v1/me"}, 0.3)...)
		}},
		{`sum by (http_route) (rate(http_server_request_duration_seconds_count{http_route!~".*/events|/v1/ws|/healthz|/readyz",http_response_status_code`, func(time.Time) []domain.Series {
			return one(map[string]string{"http_route": "/v1/search"}, 1)
		}},
		{"sum by (http_route)", func(time.Time) []domain.Series {
			return append(one(map[string]string{"http_route": "/v1/search"}, 4), one(map[string]string{"http_route": "/v1/me"}, 20)...)
		}},
		{"histogram_quantile(0.95, sum by (le) (", week(1.8, 0.3)},
		{"or vector(0)) / sum", week(0.02, 0)},
		{"sum(rate(http_server_request_duration_seconds_count", week(42, 40)},
		{"http_server_rate_limited_total", week(3, 0)},
		{"db_client_connection_acquired", week(18, 4)},
		{"db_client_connection_max", week(20, 20)},
		{"db_client_connection_wait", week(0.5, 0)},
		{"by (le, server_address)", func(time.Time) []domain.Series {
			return one(map[string]string{"server_address": "bifrost"}, 3.1)
		}},
		{"process_cpu_time_seconds_total", func(time.Time) []domain.Series {
			return one(map[string]string{"service_instance_id": "server-1"}, 0.7)
		}},
		{"container_memory_usage_bytes", func(time.Time) []domain.Series {
			return one(map[string]string{"service_instance_id": "server-1"}, 900e6)
		}},
	}}
}

func visitorLogs() *logs {
	return &logs{got: repository.Logged{Series: append(
		one(map[string]string{"visitor": "g:4f1c2d3e4a5b", "client": "other", "country": "SG"}, 900),
		one(map[string]string{"visitor": "a:0193e100", "client": "android", "country": "VN"}, 100)...)}}
}

// The health has every part with its week before, the slowest routes
// first, and the busiest visitors with their share. UC-MOD-13 main.
func TestHealth(t *testing.T) {
	t.Parallel()
	m := healthy()
	svc := service.New(service.Deps{Metrics: m, Logs: visitorLogs(), Now: func() time.Time { return now }})
	h, err := svc.Health(context.Background(), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if *h.API.P95Seconds.Now != 1.8 || *h.API.P95Seconds.WeekBefore != 0.3 || *h.API.RequestsPerS.Now != 42 || *h.RateLimitedPerS.Now != 3 {
		t.Errorf("api %+v limited %+v", h.API, h.RateLimitedPerS)
	}
	if len(h.SlowRoutes) != 2 || h.SlowRoutes[0].Route != "/v1/search" || h.SlowRoutes[0].ErrorRatio != 0.25 || h.SlowRoutes[1].RequestsPerS != 20 {
		t.Errorf("routes %+v", h.SlowRoutes)
	}
	if *h.Database.ConnectionsInUse.Now != 18 || *h.Database.ConnectionsMax != 20 || len(h.Outbound) != 1 || *h.Outbound[0].P95Seconds != 3.1 {
		t.Errorf("database %+v outbound %+v", h.Database, h.Outbound)
	}
	if len(h.Instances) != 1 || *h.Instances[0].CPUCores != 0.7 || h.Instances[0].MemoryLimitBytes != nil {
		t.Errorf("instances %+v", h.Instances)
	}
	if h.Visitors == nil || len(h.Visitors.Top) != 2 || h.Visitors.Top[0].Requests != 900 || h.Visitors.Top[0].Country != "SG" {
		t.Errorf("visitors %+v", h.Visitors)
	}
	if h.Window != "15m" || len(h.Missing) != 0 || !strings.Contains(h.Queries["api.p95_seconds"], "[15m]") {
		t.Errorf("window %s missing %v queries %v", h.Window, h.Missing, h.Queries)
	}
	for _, q := range m.ran {
		if strings.Contains(q, "[W") {
			t.Errorf("ran %s", q)
		}
	}
}

// A source down leaves its parts out and names them; every source down
// is no health. UC-MOD-13 A2, E2.
func TestHealthWithASourceDown(t *testing.T) {
	t.Parallel()
	svc := service.New(service.Deps{Metrics: healthy(), Logs: &logs{down: true}, Now: func() time.Time { return now }})
	h, err := svc.Health(context.Background(), time.Hour)
	if err != nil || h.Visitors != nil || fmt.Sprint(h.Missing) != "[visitors]" || h.API == nil {
		t.Errorf("logs down: %+v %v", h, err)
	}
	svc = service.New(service.Deps{Metrics: &metrics{down: true}, Logs: visitorLogs(), Now: func() time.Time { return now }})
	h, err = svc.Health(context.Background(), time.Hour)
	if err != nil || h.API != nil || h.Database != nil || h.Visitors == nil || len(h.Missing) != 7 {
		t.Errorf("metrics down: %+v %v", h, err)
	}
	svc = service.New(service.Deps{Metrics: &metrics{down: true}, Logs: &logs{down: true}, Now: func() time.Time { return now }})
	if _, err := svc.Health(context.Background(), time.Hour); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("all down: %v", err)
	}
}

// A query runs within the limits; past them it is refused. UC-MOD-13 3, E1.
func TestQueryMetrics(t *testing.T) {
	t.Parallel()
	many := func(time.Time) []domain.Series {
		out := make([]domain.Series, 150)
		for i := range out {
			out[i] = one(map[string]string{"i": strconv.Itoa(i)}, 1)[0]
		}
		return out
	}
	svc := service.New(service.Deps{Metrics: &metrics{answers: []answer{{"up", many}}}, Now: func() time.Time { return now }})
	got, err := svc.QueryMetrics(context.Background(), service.Query{Text: "up", Range: 6 * time.Hour}, time.Second)
	if err != nil || len(got.Series) != domain.MaxSeries || !got.Truncated || *got.Step != "87s" {
		t.Errorf("a range: %d %v %v %v", len(got.Series), got.Truncated, got.Step, err)
	}
	if got, err := svc.QueryMetrics(context.Background(), service.Query{Text: "up"}, 0); err != nil || got.Step != nil {
		t.Errorf("one time: %+v %v", got, err)
	}
	if _, err := svc.QueryMetrics(context.Background(), service.Query{Text: "up", Range: 48 * time.Hour}, 0); !errors.Is(err, domain.ErrQueryInvalid) {
		t.Errorf("two days: %v", err)
	}
	if _, err := svc.QueryMetrics(context.Background(), service.Query{Text: "bad"}, 0); !errors.Is(err, domain.ErrQueryInvalid) {
		t.Errorf("bad: %v", err)
	}
}

// A log query's lines come back masked, cut at limit. UC-MOD-13 3 · NFR-PRIV-02.
func TestQueryLogs(t *testing.T) {
	t.Parallel()
	lines := []domain.LogLine{
		{At: now, Fields: map[string]string{"to": "lan@example.vn"}, Line: "otp sent to 0912345678"},
		{At: now.Add(-time.Second), Fields: map[string]string{}, Line: "request"},
		{At: now.Add(-2 * time.Second), Fields: map[string]string{}, Line: "request"},
	}
	l := &logs{got: repository.Logged{Lines: lines}}
	svc := service.New(service.Deps{Logs: l, Now: func() time.Time { return now }})
	got, err := svc.QueryLogs(context.Background(), service.Query{Text: `{service_name="server"}`}, 2)
	if err != nil || len(got.Lines) != 2 || !got.Truncated || got.Lines[0].Line != "otp sent to <phone>" || got.Lines[0].Fields["to"] != "<email>" {
		t.Errorf("lines %+v %v", got, err)
	}
	if lines[0].Fields["to"] != "lan@example.vn" || len(got.Series) != 0 {
		t.Errorf("the source's fields changed: %v", lines[0].Fields)
	}
	l.got = repository.Logged{Series: one(map[string]string{"user": "lan@example.vn"}, 3)}
	got, err = svc.QueryLogs(context.Background(), service.Query{Text: "sum(count_over_time(x[5m]))"}, 100)
	if err != nil || len(got.Lines) != 0 || got.Series[0].Labels["user"] != "<email>" {
		t.Errorf("series %+v %v", got, err)
	}
	if _, err := svc.QueryLogs(context.Background(), service.Query{Text: "x", Range: 25 * time.Hour}, 100); !errors.Is(err, domain.ErrQueryInvalid) {
		t.Errorf("25h: %v", err)
	}
}

// The metrics that hold match, a histogram once with its metadata, and the
// labels every metric has named once. UC-MOD-13 A1.
func TestListMetrics(t *testing.T) {
	t.Parallel()
	svc := service.New(service.Deps{Metrics: &metrics{}, Now: func() time.Time { return now }})
	got, err := svc.ListMetrics(context.Background(), "http_server")
	if err != nil || len(got.Metrics) != 1 || got.Truncated {
		t.Fatalf("%+v %v", got, err)
	}
	if m := got.Metrics[0]; m.Name != "http_server_request_duration_seconds" || m.Type != "histogram" || m.Help == "" ||
		fmt.Sprint(m.Labels) != "[http_route job le]" || len(got.CommonLabels) != 0 {
		t.Errorf("the histogram %+v %v", m, got.CommonLabels)
	}
	all, err := svc.ListMetrics(context.Background(), "")
	if err != nil || fmt.Sprint(all.CommonLabels) != "[job]" || len(all.Metrics) != 5 {
		t.Fatalf("all %+v %v", all, err)
	}
	want := map[string]string{
		"db_client_connection_max": "gauge [] []", "http_server_request_duration_seconds": "histogram [http_route le] []",
		"process_cpu_time_seconds_total": "counter [] []", "traces_spanmetrics_calls_total": "counter [span_name] [job]", "up": "gauge [] []",
	}
	for _, m := range all.Metrics {
		if got := m.Type + " " + fmt.Sprint(m.Labels) + " " + fmt.Sprint(m.Lacks); got != want[m.Name] {
			t.Errorf("%s: %q, want %q", m.Name, got, want[m.Name])
		}
	}
}

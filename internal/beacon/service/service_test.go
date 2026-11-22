package service_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/shopnexus/server/internal/beacon/service"
)

// collect installs a meter provider for the test and returns a reader of
// what New's instruments recorded.
func collect(t *testing.T) func() map[string]metricdata.Aggregation {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		mp.Shutdown(context.Background())
	})
	return func() map[string]metricdata.Aggregation {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatal(err)
		}
		out := map[string]metricdata.Aggregation{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out[m.Name] = m.Data
			}
		}
		return out
	}
}

func value(attrs attribute.Set, key string) string {
	v, _ := attrs.Value(attribute.Key(key))
	return v.String()
}

//nolint:paralleltest // The test installs the global meter provider.
func TestRecordTurnsEventsIntoMetricsAndLines(t *testing.T) {
	read := collect(t)
	var logs bytes.Buffer
	b := service.New(service.Deps{
		Route: func(method, path string) string {
			if method == "GET" && path == "/v1/listings/42" {
				return "/v1/listings/{id}"
			}
			return ""
		},
		Log: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	search := uuid.NewV7()
	b.Record(t.Context(), service.Batch{App: "android", Events: []service.Event{
		{Type: "error", Message: "boom", Page: "/search"},
		{Type: "vital", Name: "LCP", Value: 1800},
		{Type: "vital", Name: "CLS", Value: 0.05},
		{Type: "request", Method: "GET", Path: "/v1/listings/42", Status: 200, DurationMs: 120, Network: "cellular"},
		{Type: "request", Method: "GET", Path: "/nope", Status: 0, DurationMs: 5000, TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"},
		{Type: "search_impression", SearchID: search, ListingIDs: []uuid.UUID{uuid.NewV7(), uuid.NewV7()}, Offset: 2},
		{Type: "search_click", SearchID: search, ListingID: uuid.NewV7(), Position: 7},
	}})

	m := read()
	requests := m["client.request.duration"].(metricdata.Histogram[float64]).DataPoints
	routes := map[string]bool{}
	for _, dp := range requests {
		routes[value(dp.Attributes, "http.route")] = true
	}
	if !routes["/v1/listings/{id}"] || !routes["other"] || len(routes) != 2 {
		t.Fatalf("routes %v", routes)
	}
	vital := m["client.web_vital.duration"].(metricdata.Histogram[float64]).DataPoints[0]
	if vital.Sum != 1.8 || value(vital.Attributes, "name") != "LCP" {
		t.Fatalf("LCP in seconds: %v %v", vital.Sum, vital.Attributes)
	}
	if got := m["client.web_vital.cls"].(metricdata.Histogram[float64]).DataPoints[0].Sum; got != 0.05 {
		t.Fatalf("CLS %v", got)
	}
	positions := map[string]int64{}
	for _, dp := range m["client.search.impressions"].(metricdata.Sum[int64]).DataPoints {
		positions[value(dp.Attributes, "position")] += dp.Value
	}
	if positions["3"] != 1 || positions["4-5"] != 1 {
		t.Fatalf("impressions by position %v", positions)
	}
	click := m["client.search.clicks"].(metricdata.Sum[int64]).DataPoints[0]
	if value(click.Attributes, "position") != "6-10" || value(click.Attributes, "app") != "android" {
		t.Fatalf("click %v", click.Attributes)
	}

	out := logs.String()
	for _, want := range []string{`msg="client error"`, "error=boom", `msg="search click"`, "server_trace_id=4bf92f3577b34da6a3ce929d0e0e4736"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs lack %s:\n%s", want, out)
		}
	}
	if strings.Count(out, `msg="client request"`) != 1 {
		t.Fatalf("only the failed, slow request gets a line:\n%s", out)
	}
}

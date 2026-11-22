package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/shopnexus/server/internal/platform/telemetry"
)

// A count gauge reads its query once per CountEvery, keeps the last answer
// through a failed read, warns once per run of failures, and reports 0 for
// a key the query left out.
func TestCountGaugesReadOncePerMinute(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	c := telemetry.CountGauges{
		Meter: mp.Meter("test"), Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(&logs, nil)),
	}

	reads, open := 0, 3
	var fail error
	telemetry.GaugeBy(c, "listings", "", "status", []string{"active", "sold"}, func(context.Context) (map[string]int, error) {
		reads++
		return map[string]int{"active": open}, fail
	})
	c.Gauge("tickets.open", "", func(context.Context) (int, error) { return open, fail })
	collect := func() map[string]int64 {
		t.Helper()
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &rm); err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				g, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("%s is %T", m.Name, m.Data)
				}
				for _, p := range g.DataPoints {
					out[m.Name+"{"+p.Attributes.Encoded(attribute.DefaultEncoder())+"}"] = p.Value
				}
			}
		}
		return out
	}

	want := map[string]int64{"listings{status=active}": 3, "listings{status=sold}": 0, "tickets.open{}": 3}
	if got := collect(); !maps.Equal(got, want) || reads != 1 {
		t.Fatalf("first: %v after %d reads", got, reads)
	}
	open = 5
	now = now.Add(50 * time.Second)
	if got := collect(); !maps.Equal(got, want) || reads != 1 {
		t.Fatalf("within the minute: %v after %d reads", got, reads)
	}
	fail = errors.New("database down")
	for range 2 {
		now = now.Add(time.Minute)
		if got := collect(); !maps.Equal(got, want) {
			t.Fatalf("failed read: %v", got)
		}
	}
	if n := strings.Count(logs.String(), "count gauge read failed"); reads != 3 || n != 2 {
		t.Fatalf("%d reads, %d warnings; want 3 and one per gauge", reads, n)
	}
	fail = nil
	now = now.Add(time.Minute)
	want = map[string]int64{"listings{status=active}": 5, "listings{status=sold}": 0, "tickets.open{}": 5}
	if got := collect(); !maps.Equal(got, want) || reads != 4 {
		t.Fatalf("after recovery: %v after %d reads", got, reads)
	}
}

// A gauge whose first read fails reports nothing rather than 0.
func TestCountGaugeWithoutAnAnswer(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	c := telemetry.CountGauges{Meter: mp.Meter("test"), Now: time.Now, Log: slog.New(slog.DiscardHandler)}
	c.Gauge("tickets.open", "", func(context.Context) (int, error) { return 0, errors.New("database down") })
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); !ok || len(g.DataPoints) != 0 {
				t.Fatalf("%s: %v", m.Name, m.Data)
			}
		}
	}
}

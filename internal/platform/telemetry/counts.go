package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// CountEvery is how often a count gauge runs its query again. The exporter
// collects every 10 s, and between reads the gauge repeats the last answer.
const CountEvery = time.Minute

// CountGauges registers gauges of how many rows a module holds now, each
// read from the database at most once per CountEvery.
type CountGauges struct {
	Meter metric.Meter
	Now   func() time.Time
	Log   *slog.Logger
}

// Gauge registers gauge name with the number read returns.
func (c CountGauges) Gauge(name, desc string, read func(context.Context) (int, error)) {
	g := Must(c.Meter.Int64ObservableGauge(name, metric.WithDescription(desc)))
	last := &lastCount[int]{name: name, read: read, now: c.Now, log: c.Log}
	Must(c.Meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		if n, ok := last.get(ctx); ok {
			o.ObserveInt64(g, int64(n))
		}
		return nil
	}, g))
}

// GaugeBy registers gauge name with one series per key, labelled label; a
// key read leaves out is 0.
func GaugeBy[K ~string](c CountGauges, name, desc, label string, keys []K, read func(context.Context) (map[K]int, error)) {
	g := Must(c.Meter.Int64ObservableGauge(name, metric.WithDescription(desc)))
	last := &lastCount[map[K]int]{name: name, read: read, now: c.Now, log: c.Log}
	Must(c.Meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		counts, ok := last.get(ctx)
		if !ok {
			return nil
		}
		for _, k := range keys {
			o.ObserveInt64(g, int64(counts[k]), metric.WithAttributes(attribute.String(label, string(k))))
		}
		return nil
	}, g))
}

// lastCount keeps read's last answer. A failed read keeps the old answer
// and logs once, until a read succeeds again.
type lastCount[T any] struct {
	name string
	read func(context.Context) (T, error)
	now  func() time.Time
	log  *slog.Logger

	mu     sync.Mutex
	value  T
	ok     bool
	tried  time.Time
	failed bool
}

// get returns the last answer and whether there is one, reading again
// when CountEvery has passed since the last try.
func (l *lastCount[T]) get(ctx context.Context) (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := l.now(); l.tried.IsZero() || now.Sub(l.tried) >= CountEvery {
		l.tried = now
		v, err := l.read(ctx)
		switch {
		case err == nil:
			l.value, l.ok, l.failed = v, true, false
		case !l.failed:
			l.failed = true
			l.log.WarnContext(ctx, "count gauge read failed", "gauge", l.name, "err", err)
		default:
			// This run of failures is logged already.
		}
	}
	return l.value, l.ok
}

// Package postgres opens the pgx pool, runs migrations and traces every
// query. Transactions belong to the stores that need them.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server/internal/platform/telemetry"
)

// NewPool opens a pool on url with the query tracer and pool metrics
// installed, and checks that the database answers.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = tracer{}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	registerPoolMetrics(pool)
	return pool, nil
}

func registerPoolMetrics(pool *pgxpool.Pool) {
	m := otel.Meter("github.com/shopnexus/server/internal/platform/postgres")
	acquired := telemetry.Must(m.Int64ObservableGauge("db.client.connection.acquired", metric.WithDescription("Connections in use.")))
	idle := telemetry.Must(m.Int64ObservableGauge("db.client.connection.idle", metric.WithDescription("Idle connections.")))
	total := telemetry.Must(m.Int64ObservableGauge("db.client.connection.total", metric.WithDescription("Open connections.")))
	maxConns := telemetry.Must(m.Int64ObservableGauge("db.client.connection.max", metric.WithDescription("Most connections the pool opens.")))
	wait := telemetry.Must(m.Float64ObservableCounter("db.client.connection.wait_time", metric.WithUnit("s"),
		metric.WithDescription("Total time spent waiting to acquire a connection.")))
	telemetry.Must(m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(acquired, int64(s.AcquiredConns()))
		o.ObserveInt64(idle, int64(s.IdleConns()))
		o.ObserveInt64(total, int64(s.TotalConns()))
		o.ObserveInt64(maxConns, int64(s.MaxConns()))
		o.ObserveFloat64(wait, s.AcquireDuration().Seconds())
		return nil
	}, acquired, idle, total, maxConns, wait))
}

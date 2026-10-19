package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
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
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := registerPoolMetrics(pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func registerPoolMetrics(pool *pgxpool.Pool) error {
	m := otel.Meter("github.com/shopnexus/server2/internal/platform/postgres")
	acquired, err := m.Int64ObservableGauge("db.client.connection.acquired", metric.WithDescription("Connections in use."))
	if err != nil {
		return err
	}
	idle, err := m.Int64ObservableGauge("db.client.connection.idle", metric.WithDescription("Idle connections."))
	if err != nil {
		return err
	}
	total, err := m.Int64ObservableGauge("db.client.connection.total", metric.WithDescription("Open connections."))
	if err != nil {
		return err
	}
	wait, err := m.Float64ObservableCounter("db.client.connection.wait_time", metric.WithUnit("s"),
		metric.WithDescription("Total time spent waiting to acquire a connection."))
	if err != nil {
		return err
	}
	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(acquired, int64(s.AcquiredConns()))
		o.ObserveInt64(idle, int64(s.IdleConns()))
		o.ObserveInt64(total, int64(s.TotalConns()))
		o.ObserveFloat64(wait, s.AcquireDuration().Seconds())
		return nil
	}, acquired, idle, total, wait)
	return err
}

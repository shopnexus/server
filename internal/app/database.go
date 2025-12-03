package app

import (
	"context"
	"log/slog"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/pg"
	"shopnexus-remastered/internal/shared/pgsqlc"

	"go.uber.org/fx"
)

// NewPgSqlc creates a new database connection
func NewPgSqlc(lc fx.Lifecycle, cfg *config.Config) (pgsqlc.TxBeginner, error) {
	pool, err := pg.New(pg.Options{
		Url:             cfg.Postgres.Url,
		Host:            cfg.Postgres.Host,
		Port:            cfg.Postgres.Port,
		Username:        cfg.Postgres.Username,
		Password:        cfg.Postgres.Password,
		Database:        cfg.Postgres.Database,
		MaxConnections:  cfg.Postgres.MaxConnections,
		MaxConnIdleTime: cfg.Postgres.MaxConnIdleTime * time.Second,
	})
	if err != nil {
		return nil, err
	}

	// Add lifecycle hooks for cleanup
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				slog.Error("Failed to ping database", err)
				return err
			}
			slog.Info("Connected to database",
				"db", cfg.Postgres.Database,
				"host", cfg.Postgres.Host,
				"port", cfg.Postgres.Port,
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			slog.Info("Closing database connection...")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

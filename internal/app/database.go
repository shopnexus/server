package app

import (
	"context"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/pgxpool"
	"shopnexus-remastered/internal/logger"
	"shopnexus-remastered/internal/utils/pgsqlc"

	"go.uber.org/fx"
)

// NewDatabase creates a new database connection
func NewDatabase(lc fx.Lifecycle, cfg *config.Config) (pgsqlc.Storage, error) {
	pool, err := pgxpool.New(pgxpool.Options{
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
				logger.Log.Sugar().Errorf("Failed to ping database: %v", err)
				return err
			}
			logger.Log.Sugar().Infof("Connected to database %s at %s:%d",
				cfg.Postgres.Database,
				cfg.Postgres.Host,
				cfg.Postgres.Port,
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Log.Sugar().Info("Closing database connection...")
			pool.Close()
			return nil
		},
	})

	// TODO: add allow nested transaction to config
	return pgsqlc.NewTxQueries(pool, false), nil
}

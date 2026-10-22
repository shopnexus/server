// Command migrate applies pending migrations to DATABASE_URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()

	applied, err := postgres.Migrate(ctx, pool, migrations.FS)
	for _, m := range applied {
		fmt.Println("applied", m.File())
	}
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if len(applied) == 0 {
		fmt.Println("no migrations to apply")
	}
	return nil
}

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"shopnexus-remastered/config"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	// Read DB URL from env (or hardcode here if you prefer)
	dbURL := config.GetConfig().Postgres.Url
	// Optional: pass a single module name, e.g. "account" or "analytic"
	moduleFlag := flag.String("module", "", "module to migrate (if empty, migrate all modules)")
	downFlag := flag.Bool("down", false, "run down migrations (rollback) instead of up")
	flag.Parse()

	// Define your modules here in the order they should be migrated
	modules := []string{
		"account",
		"analytic",
		"catalog",
		"common",
		"inventory",
		"order",
		"promotion",
		"system",
		// add more modules here...
	}

	if *moduleFlag != "" {
		// Run for a single module
		direction := "up"
		if *downFlag {
			direction = "down"
		}
		if err := migrateModule(dbURL, *moduleFlag, *downFlag); err != nil {
			log.Fatalf("migrate %s %s failed: %v", direction, *moduleFlag, err)
		}
		log.Printf("migrate %s %s done", direction, *moduleFlag)
		return
	}

	// Run for all modules
	// When rolling back, run in reverse order
	if *downFlag {
		for i := len(modules) - 1; i >= 0; i-- {
			m := modules[i]
			log.Printf("rolling back module %s...", m)
			if err := migrateModule(dbURL, m, true); err != nil {
				log.Fatalf("migrate down %s failed: %v", m, err)
			}
			log.Printf("migrate down %s done", m)
		}
	} else {
		for _, m := range modules {
			log.Printf("migrating module %s...", m)
			if err := migrateModule(dbURL, m, false); err != nil {
				log.Fatalf("migrate up %s failed: %v", m, err)
			}
			log.Printf("migrate up %s done", m)
		}
	}

}

func migrateModule(dbURL, module string, down bool) error {
	// Open DB connection
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// Create driver with module-specific migrations table
	// This ensures each module tracks its migrations separately
	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: fmt.Sprintf("schema_migrations_%s", module),
	})
	if err != nil {
		return fmt.Errorf("create driver: %w", err)
	}

	// Build path to migrations for this module
	// Example: file:///<project-root>/internal/module/account/migrations
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	migrationsPath := filepath.Join(cwd, "internal", "module", module, "db", "migrations")
	sourceURL := "file://" + filepath.ToSlash(migrationsPath)

	m, err := migrate.NewWithDatabaseInstance(
		sourceURL,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("new migrate: %w", err)
	}

	// Run migrations in the specified direction
	if down {
		// Run all down migrations (rollback)
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			return fmt.Errorf("migrate down: %w", err)
		}
	} else {
		// Run all up migrations
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			return fmt.Errorf("migrate up: %w", err)
		}
	}

	return nil
}

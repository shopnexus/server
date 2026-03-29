package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"shopnexus-server/config"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := config.GetConfig().Postgres.Url
	moduleFlag := flag.String("module", "", "module to migrate (if empty, migrate all modules)")
	downFlag := flag.Bool("down", false, "run down migrations (rollback) instead of up")
	forceFlag := flag.Int("force", -1, "force set migration version (use to fix dirty state, e.g. -force 1)")
	flag.Parse()

	// Modules ordered by dependency: common first, modules with cross-schema FKs last
	modules := []string{
		"common",
		"account",
		"catalog",
		"analytic",
		"inventory",
		"order",
		"promotion",
		"system",
		"chat", // depends on account schema
	}

	if *moduleFlag != "" {
		if *forceFlag >= 0 {
			if err := forceModule(dbURL, *moduleFlag, *forceFlag); err != nil {
				log.Fatalf("force %s to version %d failed: %v", *moduleFlag, *forceFlag, err)
			}
			log.Printf("force %s to version %d done", *moduleFlag, *forceFlag)
			return
		}
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

	// Run for all modules; rollback runs in reverse order
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

// migrationsSourceURL returns a file:// URL using a relative path.
// golang-migrate's parseURL resolves "./..." via filepath.Abs, which works cross-platform
// (avoids Windows issues with absolute file:// URLs like file:///D:/...).
func migrationsSourceURL(module string) string {
	return fmt.Sprintf("file://./internal/module/%s/db/migrations", module)
}

func newMigrate(dbURL, module string) (*migrate.Migrate, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: fmt.Sprintf("schema_migrations_%s", module),
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create driver: %w", err)
	}

	sourceURL := migrationsSourceURL(module)

	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("new migrate: %w", err)
	}

	return m, nil
}

func migrateModule(dbURL, module string, down bool) error {
	m, err := newMigrate(dbURL, module)
	if err != nil {
		return err
	}
	defer m.Close()

	if down {
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			return fmt.Errorf("migrate down: %w", err)
		}
		return nil
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		// Auto-recover from dirty state: roll back the failed version and retry
		version, dirty, verr := m.Version()
		if verr == nil && dirty {
			log.Printf("⚠ %s is dirty at version %d, forcing rollback and retrying...", module, version)
			// Force to -1 (no migrations) to clear dirty flag cleanly.
			// We can't force to version-1 because that version's down file may not exist.
			if ferr := m.Force(-1); ferr != nil {
				return fmt.Errorf("migrate up: %w (auto-fix failed: %v)", err, ferr)
			}
			// Retry
			if rerr := m.Up(); rerr != nil && rerr != migrate.ErrNoChange {
				return fmt.Errorf("migrate up (retry): %w", rerr)
			}
			log.Printf("✓ %s recovered from dirty state and migrated successfully", module)
			return nil
		}
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}

func forceModule(dbURL, module string, version int) error {
	m, err := newMigrate(dbURL, module)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("force version: %w", err)
	}
	return nil
}

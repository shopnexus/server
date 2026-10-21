// Package pgtest gives each test its own migrated Postgres database, cloned
// from a template in milliseconds, on the test stack only. Under
// `go test -short` tests that need Postgres skip; without -short a missing
// TEST_DATABASE_URL fails them, so a broken setup is never mistaken for a pass.
package pgtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/migrations"
)

const (
	testPort = "56433"
	testName = "server2_test"
	// cloneLock serialises template builds and clones across the test
	// binaries `go test ./...` runs in parallel.
	cloneLock = 7_301_002
)

// guard refuses any database but the test stack's, so a test run can never
// drop development data.
func guard(adminURL string) error {
	u, err := url.Parse(adminURL)
	if err != nil {
		return fmt.Errorf("refusing: unparsable TEST_DATABASE_URL: %w", err)
	}
	if u.Port() != testPort || u.Path != "/"+testName {
		return fmt.Errorf("refusing to create test databases on %s%s: want port %s and database %s (make test)",
			u.Host, u.Path, testPort, testName)
	}
	return nil
}

// AdminURL is TEST_DATABASE_URL, or the test stack's Postgres when it is
// unset.
func AdminURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://server2:server2@localhost:" + testPort + "/" + testName + "?sslmode=disable"
}

// URL returns AdminURL, and skips the test under -short.
func URL(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Postgres; skipped under -short")
	}
	return AdminURL()
}

// New returns a pool on a fresh database with every migration applied. The
// database is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, drop, err := NewForMain(URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return pool
}

// Empty returns a pool on a fresh database with no migrations applied.
func Empty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, drop, err := create(URL(t), "template0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return pool
}

// NewForMain is New for TestMain, where no testing.TB exists.
func NewForMain(adminURL string) (*pgxpool.Pool, func(), error) {
	tpl, err := template(adminURL)
	if err != nil {
		return nil, nil, err
	}
	return create(adminURL, tpl)
}

// template returns the name of a template database holding the current
// migrations, building it when it does not exist. The name carries a hash of
// the migrations, so adding one produces a new template.
func template(adminURL string) (string, error) {
	migs, err := postgres.LoadMigrations(migrations.FS)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, m := range migs {
		h.Write(m.Checksum[:])
	}
	name := "server2_tpl_" + hex.EncodeToString(h.Sum(nil))[:12]

	err = withAdmin(adminURL, func(ctx context.Context, conn *pgx.Conn) error {
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = @name)",
			pgx.StrictNamedArgs{"name": name}).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		// Build under a temporary name and rename when done, so a run that
		// dies half way never leaves a broken template behind.
		building := name + "_building"
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+building+" WITH (FORCE)"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "CREATE DATABASE "+building+" TEMPLATE template0"); err != nil {
			return err
		}
		pool, err := pgxpool.New(ctx, withDatabase(adminURL, building))
		if err != nil {
			return err
		}
		_, err = postgres.Migrate(ctx, pool, migrations.FS)
		// CREATE DATABASE ... TEMPLATE fails while anyone is connected to
		// the template, so close before renaming and cloning.
		pool.Close()
		if err != nil {
			return fmt.Errorf("build template: %w", err)
		}
		_, err = conn.Exec(ctx, "ALTER DATABASE "+building+" RENAME TO "+name)
		return err
	})
	return name, err
}

// create clones tpl into a new database and returns a pool on it and a
// function that drops it.
func create(adminURL, tpl string) (*pgxpool.Pool, func(), error) {
	// Database names cannot be bind parameters. rand.Text is base32
	// (A-Z, 2-7), so the lower-cased name needs no quoting.
	name := "t_" + strings.ToLower(rand.Text()[:16])
	err := withAdmin(adminURL, func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tpl)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create test database: %w", err)
	}
	pool, err := pgxpool.New(context.Background(), withDatabase(adminURL, name))
	if err != nil {
		return nil, nil, err
	}
	drop := func() {
		pool.Close()
		withAdmin(adminURL, func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			return err
		})
	}
	return pool, drop, nil
}

// withAdmin runs fn on one admin connection holding cloneLock.
func withAdmin(adminURL string, fn func(context.Context, *pgx.Conn) error) error {
	if err := guard(adminURL); err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(@lock)", pgx.StrictNamedArgs{"lock": cloneLock}); err != nil {
		return err
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(@lock)", pgx.StrictNamedArgs{"lock": cloneLock})
	return fn(ctx, conn)
}

func withDatabase(adminURL, name string) string {
	u, _ := url.Parse(adminURL)
	u.Path = "/" + name
	return u.String()
}

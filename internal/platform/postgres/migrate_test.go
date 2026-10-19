package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

func files(m map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range m {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

var twoMigrations = map[string]string{
	"0001_a.sql": "CREATE TABLE a (id int);",
	"0002_b.sql": "CREATE TABLE b (id int);",
}

func TestLoadMigrationsSortsByVersion(t *testing.T) {
	migs, err := postgres.LoadMigrations(files(map[string]string{
		"0002_b.sql": "x", "0001_a.sql": "y", "README.md": "ignored", "embed.go": "ignored",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 2 || migs[0].Version != 1 || migs[0].Name != "a" || migs[1].Version != 2 || migs[1].Name != "b" {
		t.Fatalf("got %+v", migs)
	}
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	_, err := postgres.LoadMigrations(files(map[string]string{"0001_a.sql": "x", "0001_b.sql": "y"}))
	if err == nil || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadMigrationsRejectsBadName(t *testing.T) {
	_, err := postgres.LoadMigrations(files(map[string]string{"abc.sql": "x"}))
	if err == nil || !strings.Contains(err.Error(), "abc.sql") {
		t.Fatalf("err = %v", err)
	}
}

func appliedVersions(t *testing.T, pool *pgxpool.Pool) []int {
	t.Helper()
	rows, err := pool.Query(context.Background(), "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	var vs []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		vs = append(vs, v)
	}
	return vs
}

func TestMigrateAppliesAll(t *testing.T) {
	pool := pgtest.Empty(t)
	applied, err := postgres.Migrate(context.Background(), pool, files(twoMigrations))
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %d", len(applied))
	}
	if got := appliedVersions(t, pool); len(got) != 2 {
		t.Fatalf("recorded %v", got)
	}
}

func TestMigrateTwiceAppliesNothing(t *testing.T) {
	pool := pgtest.Empty(t)
	fsys := files(twoMigrations)
	if _, err := postgres.Migrate(context.Background(), pool, fsys); err != nil {
		t.Fatal(err)
	}
	applied, err := postgres.Migrate(context.Background(), pool, fsys)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second run applied %d, err %v", len(applied), err)
	}
}

func TestMigrateDetectsEditedMigration(t *testing.T) {
	pool := pgtest.Empty(t)
	if _, err := postgres.Migrate(context.Background(), pool, files(twoMigrations)); err != nil {
		t.Fatal(err)
	}
	edited := files(twoMigrations)
	edited["0001_a.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE a (id int, extra int);")}

	_, err := postgres.Migrate(context.Background(), pool, edited)
	if !errors.Is(err, postgres.ErrChecksumMismatch) || !strings.Contains(err.Error(), "0001_a.sql") {
		t.Fatalf("err = %v", err)
	}
	if _, err := postgres.Pending(context.Background(), pool, edited); !errors.Is(err, postgres.ErrChecksumMismatch) {
		t.Fatalf("Pending err = %v", err)
	}
}

func TestMigrateFailureRollsBackThatMigration(t *testing.T) {
	pool := pgtest.Empty(t)
	_, err := postgres.Migrate(context.Background(), pool, files(map[string]string{
		"0001_a.sql": "CREATE TABLE a (id int);",
		"0002_b.sql": "CREATE TABLE b (id int); CREATE TABLE broken (;",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	if got := appliedVersions(t, pool); len(got) != 1 || got[0] != 1 {
		t.Fatalf("recorded %v", got)
	}
	var exists bool
	pool.QueryRow(context.Background(), "SELECT to_regclass('b') IS NOT NULL").Scan(&exists)
	if exists {
		t.Fatal("table b from the failed migration survived")
	}
}

func TestMigrateConcurrentCallsApplyOnce(t *testing.T) {
	pool := pgtest.Empty(t)
	fsys := files(twoMigrations)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = postgres.Migrate(context.Background(), pool, fsys) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := appliedVersions(t, pool); len(got) != 2 {
		t.Fatalf("recorded %v", got)
	}
}

func TestPendingListsUnapplied(t *testing.T) {
	pool := pgtest.Empty(t)
	if _, err := postgres.Migrate(context.Background(), pool, files(map[string]string{"0001_a.sql": "CREATE TABLE a (id int);"})); err != nil {
		t.Fatal(err)
	}
	pending, err := postgres.Pending(context.Background(), pool, files(twoMigrations))
	if err != nil || len(pending) != 1 || pending[0].Version != 2 {
		t.Fatalf("pending %+v, err %v", pending, err)
	}
}

func TestPendingOnEmptyDatabase(t *testing.T) {
	pool := pgtest.Empty(t)
	pending, err := postgres.Pending(context.Background(), pool, files(twoMigrations))
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending %d, err %v", len(pending), err)
	}
}

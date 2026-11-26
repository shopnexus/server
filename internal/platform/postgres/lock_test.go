package postgres_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestQueueLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	id := uuid.NewV7()
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		postgres.QueueLock(b, "a", id)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err
		}
		free := func(key string) bool {
			var ok bool
			if err := db.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended(@key, 0))`,
				pgx.StrictNamedArgs{"key": key}).Scan(&ok); err != nil {
				t.Fatal(err)
			}
			if ok {
				_, _ = db.Exec(ctx, `SELECT pg_advisory_unlock_all()`)
			}
			return ok
		}
		if free("a:" + id.String()) {
			t.Error("the held lock is free")
		}
		if !free("b:" + id.String()) {
			t.Error("another scope shares the lock")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Every advisory lock goes through QueueLock, so each has a scope.
func TestNoRawAdvisoryLock(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, filepath.Join("postgres", "lock.go")) {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "pg_advisory_xact_lock") {
			t.Errorf("%s takes an advisory lock without postgres.QueueLock", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWithLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	key := "with-lock:" + uuid.NewV7().String()
	// free tries the lock on a connection of its own, as another replica.
	free := func() bool {
		conn, err := db.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		var ok bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended(@key, 0))`,
			pgx.StrictNamedArgs{"key": key}).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		if ok {
			_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended(@key, 0))`, pgx.StrictNamedArgs{"key": key})
		}
		return ok
	}
	ran := false
	if err := postgres.WithLock(ctx, db, key, func(context.Context) error {
		ran = true
		if free() {
			t.Error("the lock was free while held")
		}
		return nil
	}); err != nil || !ran {
		t.Fatalf("WithLock = %v, ran %v", err, ran)
	}
	if !free() {
		t.Error("the lock stayed held")
	}
}

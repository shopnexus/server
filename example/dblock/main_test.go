package main_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTxBlocking(t *testing.T) {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, "postgres://shopnexus:peakshopnexuspassword@localhost:5432/shopnexus")
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// tx1 locks the row
	tx1, _ := db.Begin(ctx)
	t.Log("tx1: locking row 13...")
	var id1 int
	if err = tx1.QueryRow(ctx, `
        SELECT id FROM inventory.sku_serial
        WHERE status = 'Active'
        FOR UPDATE SKIP LOCKED
        LIMIT 1;
    `).Scan(&id1); err != nil {
		t.Fatalf("tx1 failed to select: %v", err)
	}
	t.Logf("tx1: locked row %d", id1)

	// Start tx2 in separate goroutine so it runs concurrently
	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		tx2, _ := db.Begin(ctx)
		defer tx2.Rollback(ctx)

		t.Log("tx2: trying to lock row 13 (should block until tx1 commits)...")
		var id2 int
		err := tx2.QueryRow(ctx, `
            SELECT id FROM inventory.sku_serial
            WHERE status = 'Active'
            FOR UPDATE SKIP LOCKED
            LIMIT 1;
        `).Scan(&id2)
		if err == nil {
			t.Logf("tx2: acquired lock on row %d", id2)
		}
		t.Logf("tx2: acquired row %d", id2)
		errCh <- err
	}()

	// Give tx2 time to try to acquire the lock (it should be blocked)
	time.Sleep(2 * time.Second)

	select {
	case <-errCh:
		t.Fatalf("tx2 should still be waiting, but it returned early")
	default:
		t.Log("tx2 is blocked as expected")
	}

	// Commit tx1 to release the lock
	t.Log("tx1: committing (unlocking row)...")
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("tx1 commit failed: %v", err)
	}

	// Now tx2 should proceed and return
	err = <-errCh
	if err != nil {
		t.Fatalf("tx2 failed after tx1 commit: %v", err)
	}

	elapsed := time.Since(start)
	t.Logf("tx2 waited for %s until tx1 committed", elapsed)
}

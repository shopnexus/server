package main_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsn = "postgres://shopnexus:peakshopnexuspassword@localhost:5432/shopnexus"

// setupTestTable creates a temporary table for testing and returns a cleanup function.
func setupTestTable(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _test_nested_tx (
			id   SERIAL PRIMARY KEY,
			name TEXT NOT NULL
		);
		TRUNCATE _test_nested_tx;
	`)
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
}

// countRows returns the number of rows in the test table.
func countRows(t *testing.T, ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) int {
	t.Helper()
	var count int
	if err := querier.QueryRow(ctx, `SELECT COUNT(*) FROM _test_nested_tx`).Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	return count
}

// insertRow inserts a row and returns its ID.
func insertRow(t *testing.T, ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string) int {
	t.Helper()
	var id int
	if err := querier.QueryRow(ctx, `INSERT INTO _test_nested_tx (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("failed to insert row %q: %v", name, err)
	}
	t.Logf("inserted row id=%d name=%q", id, name)
	return id
}

func connect(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

// TestNestedTx_InnerRollback_OuterCommit verifies that rolling back an inner
// (nested) transaction via savepoint does NOT affect the outer transaction.
//
// Flow:
//   outer BEGIN
//     INSERT "outer_row"
//     inner BEGIN  (SAVEPOINT)
//       INSERT "inner_row"
//     inner ROLLBACK (ROLLBACK TO SAVEPOINT)  ← "inner_row" is undone
//   outer COMMIT                              ← "outer_row" persists
//
// Expected: only "outer_row" remains.
func TestNestedTx_InnerRollback_OuterCommit(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	// Begin outer transaction
	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "outer_row")
	t.Logf("after outer insert: %d rows visible inside outer tx", countRows(t, ctx, outerTx))

	// Begin inner (nested) transaction — pgx uses SAVEPOINT under the hood
	innerTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin inner tx: %v", err)
	}

	insertRow(t, ctx, innerTx, "inner_row")
	t.Logf("after inner insert: %d rows visible inside inner tx", countRows(t, ctx, innerTx))

	// Rollback inner transaction (ROLLBACK TO SAVEPOINT)
	if err := innerTx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback inner tx: %v", err)
	}
	t.Log("inner tx rolled back")

	// Outer tx should still see only "outer_row"
	outerCount := countRows(t, ctx, outerTx)
	t.Logf("after inner rollback: %d rows visible inside outer tx", outerCount)
	if outerCount != 1 {
		t.Errorf("expected 1 row after inner rollback, got %d", outerCount)
	}

	// Commit outer transaction
	if err := outerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit outer tx: %v", err)
	}
	t.Log("outer tx committed")

	// Verify from a fresh connection
	finalCount := countRows(t, ctx, db)
	t.Logf("final count from pool: %d", finalCount)
	if finalCount != 1 {
		t.Errorf("expected 1 row after outer commit, got %d", finalCount)
	}
}

// TestNestedTx_InnerCommit_OuterRollback verifies that even if the inner
// (nested) transaction commits (releases savepoint), rolling back the outer
// transaction undoes EVERYTHING — including the inner changes.
//
// Flow:
//   outer BEGIN
//     INSERT "outer_row"
//     inner BEGIN  (SAVEPOINT)
//       INSERT "inner_row"
//     inner COMMIT (RELEASE SAVEPOINT)  ← savepoint released, but still inside outer tx
//   outer ROLLBACK                      ← everything is undone
//
// Expected: 0 rows remain.
func TestNestedTx_InnerCommit_OuterRollback(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "outer_row")

	// Inner tx (savepoint)
	innerTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin inner tx: %v", err)
	}

	insertRow(t, ctx, innerTx, "inner_row")

	// Commit inner — this just releases the savepoint
	if err := innerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit inner tx: %v", err)
	}
	t.Log("inner tx committed (savepoint released)")

	// Both rows should be visible inside outer tx
	outerCount := countRows(t, ctx, outerTx)
	t.Logf("after inner commit: %d rows visible inside outer tx", outerCount)
	if outerCount != 2 {
		t.Errorf("expected 2 rows inside outer tx after inner commit, got %d", outerCount)
	}

	// Rollback outer — should undo everything
	if err := outerTx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback outer tx: %v", err)
	}
	t.Log("outer tx rolled back")

	// Verify nothing persisted
	finalCount := countRows(t, ctx, db)
	t.Logf("final count from pool: %d", finalCount)
	if finalCount != 0 {
		t.Errorf("expected 0 rows after outer rollback, got %d", finalCount)
	}
}

// TestNestedTx_BothCommit verifies that when both inner and outer commit,
// all rows persist.
func TestNestedTx_BothCommit(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "outer_row")

	innerTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin inner tx: %v", err)
	}

	insertRow(t, ctx, innerTx, "inner_row")

	if err := innerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit inner tx: %v", err)
	}
	t.Log("inner tx committed")

	if err := outerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit outer tx: %v", err)
	}
	t.Log("outer tx committed")

	finalCount := countRows(t, ctx, db)
	t.Logf("final count from pool: %d", finalCount)
	if finalCount != 2 {
		t.Errorf("expected 2 rows after both commit, got %d", finalCount)
	}
}

// TestNestedTx_BothRollback verifies that rolling back both inner and outer
// leaves zero rows.
func TestNestedTx_BothRollback(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "outer_row")

	innerTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin inner tx: %v", err)
	}

	insertRow(t, ctx, innerTx, "inner_row")

	if err := innerTx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback inner tx: %v", err)
	}
	t.Log("inner tx rolled back")

	if err := outerTx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback outer tx: %v", err)
	}
	t.Log("outer tx rolled back")

	finalCount := countRows(t, ctx, db)
	t.Logf("final count from pool: %d", finalCount)
	if finalCount != 0 {
		t.Errorf("expected 0 rows after both rollback, got %d", finalCount)
	}
}

// TestNestedTx_TripleNesting verifies 3 levels of nesting (2 savepoints).
//
// Flow:
//   outer BEGIN
//     INSERT "L1"
//     mid BEGIN (SAVEPOINT sp1)
//       INSERT "L2"
//       inner BEGIN (SAVEPOINT sp2)
//         INSERT "L3"
//       inner ROLLBACK (ROLLBACK TO sp2)  ← "L3" undone
//     mid COMMIT (RELEASE sp1)            ← "L2" survives
//   outer COMMIT                          ← "L1" + "L2" persist
//
// Expected: 2 rows ("L1", "L2").
func TestNestedTx_TripleNesting(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "L1")

	midTx, err := outerTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin mid tx: %v", err)
	}

	insertRow(t, ctx, midTx, "L2")

	innerTx, err := midTx.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin inner tx: %v", err)
	}

	insertRow(t, ctx, innerTx, "L3")
	t.Logf("3 levels deep: %d rows visible", countRows(t, ctx, innerTx))

	// Rollback innermost — "L3" undone
	if err := innerTx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback inner tx: %v", err)
	}
	t.Log("inner (L3) tx rolled back")

	midCount := countRows(t, ctx, midTx)
	t.Logf("after inner rollback: %d rows visible in mid tx", midCount)
	if midCount != 2 {
		t.Errorf("expected 2 rows in mid tx, got %d", midCount)
	}

	// Commit mid — releases savepoint
	if err := midTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit mid tx: %v", err)
	}
	t.Log("mid (L2) tx committed")

	// Commit outer
	if err := outerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit outer tx: %v", err)
	}
	t.Log("outer (L1) tx committed")

	finalCount := countRows(t, ctx, db)
	t.Logf("final count: %d", finalCount)
	if finalCount != 2 {
		t.Errorf("expected 2 rows (L1, L2), got %d", finalCount)
	}
}

// TestNestedTx_ErrorInInner_OuterContinues verifies that an error in an inner
// transaction can be handled gracefully — the outer transaction continues working
// after the inner savepoint is rolled back.
func TestNestedTx_ErrorInInner_OuterContinues(t *testing.T) {
	ctx := context.Background()
	db := connect(t)
	setupTestTable(t, ctx, db)

	outerTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin outer tx: %v", err)
	}

	insertRow(t, ctx, outerTx, "before_error")

	// Simulate a business operation that fails in a nested tx
	func() {
		innerTx, err := outerTx.Begin(ctx)
		if err != nil {
			t.Fatalf("failed to begin inner tx: %v", err)
		}
		defer innerTx.Rollback(ctx)

		insertRow(t, ctx, innerTx, "will_be_undone")

		// Simulate a business error
		bizErr := fmt.Errorf("insufficient stock")
		if bizErr != nil {
			t.Logf("inner tx encountered error: %v, rolling back", bizErr)
			return // triggers deferred Rollback
		}

		// This won't execute
		innerTx.Commit(ctx)
	}()

	// Outer tx should still work fine after inner rollback
	insertRow(t, ctx, outerTx, "after_error")

	outerCount := countRows(t, ctx, outerTx)
	t.Logf("outer tx has %d rows (expecting 2: before_error, after_error)", outerCount)
	if outerCount != 2 {
		t.Errorf("expected 2 rows, got %d", outerCount)
	}

	if err := outerTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit outer tx: %v", err)
	}

	finalCount := countRows(t, ctx, db)
	t.Logf("final count: %d", finalCount)
	if finalCount != 2 {
		t.Errorf("expected 2 persisted rows, got %d", finalCount)
	}
}

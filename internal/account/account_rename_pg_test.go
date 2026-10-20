package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

// A suspension committed while a rename waits must survive the rename, and
// the rename must see it and refuse.
func TestRenameWaitsForConcurrentSuspension(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	store := account.NewPGStore()
	a, err := account.New("Linh", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(ctx, pool, &a); err != nil {
		t.Fatal(err)
	}

	admin, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Rollback(ctx)
	if _, err := admin.Exec(ctx,
		`UPDATE accounts SET status = 'suspended', suspend_reason = 'spam' WHERE id = $1`, a.ID()); err != nil {
		t.Fatal(err)
	}

	uc := account.AccountRename{Store: store, Tx: postgres.NewTxRunner(pool), Now: time.Now}
	done := make(chan error, 1)
	go func() {
		_, err := uc.Execute(ctx, a.ID(), account.AccountRenameInput{DisplayName: "Minh"})
		done <- err
	}()

	// Commit only once the rename is blocked on the admin's row lock.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`,
		).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rename never waited for the row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := admin.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; !errors.Is(err, account.ErrSuspended) {
		t.Fatalf("rename err %v, want ErrSuspended", err)
	}
	got, err := store.Get(ctx, pool, a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status(time.Now()) != account.StatusSuspended || got.DisplayName() != "Linh" {
		t.Fatalf("after rename: status %s, name %q", got.Status(time.Now()), got.DisplayName())
	}
}

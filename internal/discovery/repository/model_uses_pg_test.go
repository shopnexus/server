package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestModelUses(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	u := repository.NewModelUses(db)
	me := accounttest.New(t, db)
	day := time.Date(2026, 11, 23, 23, 0, 0, 0, time.UTC)
	for i, want := range []bool{true, true, false} {
		if ok, err := u.Take(ctx, me, day, 2); err != nil || ok != want {
			t.Fatalf("call %d: %v %v, want %v", i, ok, err, want)
		}
	}
	if ok, err := u.Take(ctx, me, day.Add(2*time.Hour), 2); err != nil || !ok {
		t.Fatalf("the next UTC day starts again: %v %v", ok, err)
	}
	if err := u.Clear(ctx, me); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM search_model_uses WHERE account_id = $1`, me).Scan(&n); err != nil || n != 0 {
		t.Fatalf("clear left %d rows: %v", n, err)
	}
}

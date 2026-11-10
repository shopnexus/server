package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func watch(account uuid.UUID, q string) repository.Watch {
	return repository.Watch{
		ID: uuid.NewV7(), Account: account, Query: q, Phrases: []string{q}, Conditions: []string{},
		Alert: true, CreatedAt: now,
	}
}

func TestWatchPutKeepsOnePerKey(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ws := repository.NewWatches(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	first, created, err := ws.Put(ctx, watch(me, "Xe Đạp"))
	if err != nil || !created {
		t.Fatalf("%v %v", created, err)
	}
	if _, err = db.Exec(ctx, `UPDATE search_watches SET new_count = 4 WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	again := watch(me, "xe dap")
	again.PriceMax, again.Commune, again.RadiusKm = 3_000_000, "00004", 5
	got, created, err := ws.Put(ctx, again)
	if err != nil || created || got.ID != first.ID || got.PriceMax != 3_000_000 || got.RadiusKm != 5 || got.NewCount != 0 || got.Query != "xe dap" {
		t.Fatalf("same key: %+v %v %v", got, created, err)
	}
	id, found, err := ws.ByKey(ctx, me, domain.HistoryKey("XE ĐẠP"))
	if err != nil || !found || id != first.ID {
		t.Fatalf("by key: %v %v %v", id, found, err)
	}
	if found, _ = ws.SetAlert(ctx, me, first.ID, false); !found {
		t.Fatal("mute")
	}
	if found, _ = ws.SetAlert(ctx, accounttest.New(t, db), first.ID, false); found {
		t.Fatal("someone else's watch")
	}
	if l, _ := ws.List(ctx, me); len(l) != 1 || l[0].Alert {
		t.Fatalf("%+v", l)
	}
	if err = ws.Delete(ctx, me, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, _ = ws.Get(ctx, first.ID); found {
		t.Fatal("deleted")
	}
}

// Twenty-one watches racing: twenty are stored.
func TestWatchLimit(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ws := repository.NewWatches(db)
	me := accounttest.New(t, db)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		limit int
	)
	for i := range domain.MaxWatches + 1 {
		wg.Go(func() {
			_, _, err := ws.Put(context.Background(), watch(me, "tìm "+string(rune('a'+i))))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, domain.ErrWatchLimit):
				limit++
			case err != nil:
				t.Error(err)
			default:
			}
		})
	}
	wg.Wait()
	if l, _ := ws.List(context.Background(), me); len(l) != domain.MaxWatches || limit != 1 {
		t.Fatalf("%d stored, %d refused", len(l), limit)
	}
}

func arrive(t *testing.T, db *pgxpool.Pool, ws repository.Watches, n int) []uuid.UUID {
	t.Helper()
	ls := listings(t, db, n)
	for _, l := range ls {
		for range 2 { // a redelivery adds nothing
			if err := ws.Arrive(context.Background(), l); err != nil {
				t.Fatal(err)
			}
		}
	}
	return ls
}

func TestArrivalsAndSwept(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ws := repository.NewWatches(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	before := arrive(t, db, ws, 1)
	w, _, err := ws.Put(ctx, watch(me, "xe đạp"))
	if err != nil {
		t.Fatal(err)
	}
	ls := arrive(t, db, ws, 3)
	if a, _ := ws.Arrivals(ctx, w.LastArrival, time.Minute, 10); len(a) != 0 {
		t.Fatalf("younger than the lag: %v", a)
	}
	a, err := ws.Arrivals(ctx, w.LastArrival, 0, 2)
	if err != nil || len(a) != 2 || a[0].ListingID != ls[0] || a[1].ListingID != ls[1] || a[0].ListingID == before[0] {
		t.Fatalf("after the watch, in order, at most 2: %v %v", a, err)
	}
	moved, err := ws.Swept(ctx, w.ID, w.LastArrival, a[1].Seq, 2, now)
	if err != nil || !moved {
		t.Fatal(err)
	}
	if moved, _ = ws.Swept(ctx, w.ID, w.LastArrival, a[1].Seq, 2, now); moved {
		t.Fatal("a replay from the old mark moves nothing")
	}
	got, _, _ := ws.Get(ctx, w.ID)
	if got.NewCount != 2 || got.LastArrival != a[1].Seq {
		t.Fatalf("%+v", got)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'search_watch.matched' AND subject_id = $1
AND (payload->>'count')::int = 2`, w.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if _, err = ws.Swept(ctx, w.ID, got.LastArrival, got.LastArrival+1, 200, now); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = ws.Get(ctx, w.ID); got.NewCount != domain.MaxNewCount {
		t.Fatalf("capped: %d", got.NewCount)
	}
	if err = ws.Seen(ctx, me, w.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = ws.Get(ctx, w.ID); got.NewCount != 0 {
		t.Fatalf("seen: %d", got.NewCount)
	}
	if _, err = ws.SetAlert(ctx, me, w.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _, _ = ws.Get(ctx, w.ID)
	if moved, _ = ws.Swept(ctx, w.ID, got.LastArrival, got.LastArrival+1, 1, now); !moved {
		t.Fatal("muted still counts")
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'search_watch.matched' AND subject_id = $1`, w.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("muted writes no event: %d %v", n, err)
	}
	if _, err = db.Exec(ctx, `UPDATE listing_arrivals SET arrived_at = now() - interval '3 days' WHERE listing_id = $1`, before[0]); err != nil {
		t.Fatal(err)
	}
	arrive(t, db, ws, 1)
	if err = db.QueryRow(ctx, `SELECT count(*) FROM listing_arrivals WHERE listing_id = $1`, before[0]).Scan(&n); err != nil || n != 0 {
		t.Fatalf("an arrival past two days is deleted: %d %v", n, err)
	}
}

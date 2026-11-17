package repository_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// A person counts once per listing and day, again on a later day, and the
// counts sum all days and the days from a given one.
func TestViewsCountOncePerDay(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx := context.Background()
	seller, ann, bao := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	listing, other := catalogtest.Listing(t, db, seller), catalogtest.Listing(t, db, seller)
	monday := domain.ViewDay(now)
	tuesday := monday.AddDate(0, 0, 1)
	for _, r := range []struct {
		viewer uuid.UUID
		day    time.Time
	}{{ann, monday}, {ann, monday}, {bao, monday}, {ann, tuesday}, {ann, tuesday}, {ann, monday}} {
		if err := v.Record(ctx, listing, r.viewer, r.day); err != nil {
			t.Fatal(err)
		}
	}
	got, err := v.Counts(ctx, []uuid.UUID{listing, other}, tuesday)
	if err != nil {
		t.Fatal(err)
	}
	// Ann on Monday and Tuesday, Bao on Monday; Ann's Monday again after
	// Tuesday counts nothing.
	if c := got[listing]; c.All != 3 || c.Since != 1 {
		t.Fatalf("listing: %+v", c)
	}
	if _, ok := got[other]; ok || len(got) != 1 {
		t.Fatalf("a listing nobody opened: %+v", got)
	}
}

// The seller opening their own listing and a deleted account count
// nothing; an erasure forgets what a person opened and keeps the counts.
func TestViewsLeaveOutTheSellerAndTheDeleted(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx := context.Background()
	seller, ann, gone := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	day := domain.ViewDay(now)
	for _, who := range []uuid.UUID{seller, gone, ann} {
		if err := v.Record(ctx, listing, who, day); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := v.Counts(ctx, []uuid.UUID{listing}, day); err != nil || got[listing] != (repository.Counts{All: 1, Since: 1}) {
		t.Fatalf("%+v %v", got, err)
	}
	if err := v.Clear(ctx, ann); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listing_view_marks WHERE viewer_id = @ann`,
		pgx.StrictNamedArgs{"ann": ann}).Scan(&marks); err != nil || marks != 0 {
		t.Fatalf("marks after the erasure: %d %v", marks, err)
	}
	if got, err := v.Counts(ctx, []uuid.UUID{listing}, day); err != nil || got[listing].All != 1 {
		t.Fatalf("counts after the erasure: %+v %v", got, err)
	}
}

// Only an active or reserved listing counts a view, whoever calls.
func TestViewsCountPublicListingsOnly(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx := context.Background()
	seller, ann := accounttest.New(t, db), accounttest.New(t, db)
	day := domain.ViewDay(now)
	statuses := []string{"active", "reserved", "hidden", "sold", "processing", "rejected", "deleted"}
	ids := make([]uuid.UUID, 0, len(statuses))
	for _, st := range statuses {
		id := catalogtest.Listing(t, db, seller)
		if _, err := db.Exec(ctx, `UPDATE listings SET status = @status WHERE id = @id`, pgx.StrictNamedArgs{"status": st, "id": id}); err != nil {
			t.Fatal(err)
		}
		if err := v.Record(ctx, id, ann, day); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	got, err := v.Counts(ctx, ids, day)
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range statuses {
		want := 0
		if st == "active" || st == "reserved" {
			want = 1
		}
		if got[ids[i]].All != want {
			t.Errorf("a %s listing counts %d, want %d", st, got[ids[i]].All, want)
		}
	}
}

// A view in flight when its viewer's account is deleted commits before the
// erasure's Clear deletes, so its mark goes too, and a view after the
// deletion writes nothing; the deletion waits on no lock of the view's.
func TestViewsAndErasureRace(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seller, ann, bao := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	day := domain.ViewDay(now)
	// Bao's view writes the day's row; a held transaction locks it, so
	// Ann's record stops inside its statement, after it read her account.
	if err := v.Record(ctx, listing, bao, day); err != nil {
		t.Fatal(err)
	}
	hold, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err = hold.Exec(ctx, `SELECT 1 FROM listing_view_days WHERE listing_id = @id FOR UPDATE`,
		pgx.StrictNamedArgs{"id": listing}); err != nil {
		t.Fatal(err)
	}
	recorded := make(chan error, 1)
	go func() { recorded <- v.Record(ctx, listing, ann, day) }()
	waiting := func(event string) func() error {
		return func() error {
			var n int
			if qErr := db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = @event`,
				pgx.StrictNamedArgs{"event": event}).Scan(&n); qErr != nil {
				return qErr
			}
			if n == 0 {
				return fmt.Errorf("nobody waits on %s", event)
			}
			return nil
		}
	}
	testenv.Eventually(t, 10*time.Second, waiting("transactionid"))

	if _, err = db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": ann}); err != nil {
		t.Fatalf("the deletion: %v", err)
	}
	cleared := make(chan error, 1)
	go func() { cleared <- v.Clear(ctx, ann) }()
	testenv.Eventually(t, 10*time.Second, waiting("advisory"))
	if err = hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-recorded; err != nil {
		t.Fatalf("the record: %v", err)
	}
	if err = <-cleared; err != nil {
		t.Fatalf("the clear: %v", err)
	}
	if err = v.Record(ctx, listing, ann, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM listing_view_marks WHERE viewer_id = @ann`,
		pgx.StrictNamedArgs{"ann": ann}).Scan(&marks); err != nil || marks != 0 {
		t.Fatalf("Ann's marks after the erasure: %d %v", marks, err)
	}
	if got, countErr := v.Counts(ctx, []uuid.UUID{listing}, day); countErr != nil || got[listing].All != 2 {
		t.Fatalf("counts: %+v %v", got, countErr)
	}
}

// Concurrent opens of one person count once, and of different people
// count each.
func TestViewsUnderConcurrency(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx := context.Background()
	seller, ann := accounttest.New(t, db), accounttest.New(t, db)
	people := []uuid.UUID{accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)}
	listing := catalogtest.Listing(t, db, seller)
	day := domain.ViewDay(now)
	var wg sync.WaitGroup
	errs := make(chan error, 8+len(people))
	for range 8 {
		wg.Go(func() { errs <- v.Record(ctx, listing, ann, day) })
	}
	for _, p := range people {
		wg.Go(func() { errs <- v.Record(ctx, listing, p, day) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, err := v.Counts(ctx, []uuid.UUID{listing}, day); err != nil || got[listing].All != 1+len(people) {
		t.Fatalf("%+v %v", got, err)
	}
}

// Saves count by listing, all of them and those since a time.
func TestSaveCounts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	ls := listings(t, db, 2)
	early, late := accounttest.New(t, db), accounttest.New(t, db)
	if err := s.Save(ctx, uuid.NewV7(), early, ls[0], 1_500_000, now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, uuid.NewV7(), late, ls[0], 1_500_000, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.Counts(ctx, ls, now.Add(-7*24*time.Hour))
	if err != nil || got[ls[0]] != (repository.Counts{All: 2, Since: 1}) || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

// Recent lists one viewer's marks from a day on, the latest day first, up
// to a limit; a listing opened again keeps its last day only.
func TestViewsRecent(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	v := repository.NewViews(db)
	ctx := context.Background()
	seller, ann, bao := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	old, again, today := catalogtest.Listing(t, db, seller), catalogtest.Listing(t, db, seller), catalogtest.Listing(t, db, seller)
	day := domain.ViewDay(now)
	for _, r := range []struct {
		listing, viewer uuid.UUID
		day             time.Time
	}{
		{old, ann, day.AddDate(0, 0, -40)},
		{again, ann, day.AddDate(0, 0, -45)},
		{again, ann, day.AddDate(0, 0, -3)},
		{today, ann, day},
		{today, bao, day},
	} {
		if err := v.Record(ctx, r.listing, r.viewer, r.day); err != nil {
			t.Fatal(err)
		}
	}
	got, err := v.Recent(ctx, ann, day.AddDate(0, 0, -29), 10)
	if err != nil || len(got) != 2 || got[0].Listing != today || !got[0].Day.Equal(day) ||
		got[1].Listing != again || !got[1].Day.Equal(day.AddDate(0, 0, -3)) || got[0].Category != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = v.Recent(ctx, ann, day.AddDate(0, 0, -29), 1); err != nil || len(got) != 1 || got[0].Listing != today {
		t.Fatalf("a limit of 1: %+v %v", got, err)
	}
}

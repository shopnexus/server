package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/assist/repository"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

var now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func read(account uuid.UUID, at time.Time) domain.VideoRead {
	return domain.NewVideoRead(uuid.NewV7(), account, uuid.NewV7(), at)
}

func seq(t *testing.T, db *pgxpool.Pool, account uuid.UUID) int64 {
	t.Helper()
	s, err := stream.PGLog{DB: db}.Current(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInsertGetAndNotify(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewVideoReads(db)
	me := accounttest.New(t, db)
	r := read(me, now)
	if err := reads.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := reads.Insert(ctx, r); err != nil { // a retried step
		t.Fatalf("second insert: %v", err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.AccountID() != me || got.VideoID() != r.VideoID() || got.Status() != domain.StatusReading ||
		got.Frames() != nil || got.Draft() != nil || got.Estimate() != nil || !got.ExpiresAt().Equal(now.Add(24*time.Hour)) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	p, err := stream.PGLog{DB: db}.Since(ctx, me, 0, 10)
	if err != nil || len(p.Updates) != 1 || p.Updates[0].Kind != stream.KindVideoRead || *p.Updates[0].VideoReadID != r.ID() {
		t.Fatalf("one update for two inserts: %+v %v", p, err)
	}
	if _, err = reads.Get(ctx, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown read: %v", err)
	}
}

func TestSave(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewVideoReads(db)
	me := accounttest.New(t, db)
	r := read(me, now)
	if err := reads.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	frame := uuid.NewV7()
	if err := r.SetFrames([]domain.Frame{{UploadID: frame, AtSeconds: 1.5}}); err != nil {
		t.Fatal(err)
	}
	first := uuid.NewV7()
	retry := r // the journal's copy, as a replayed step holds it
	if err := reads.Save(ctx, &r, first); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &retry, first); err != nil {
		t.Fatalf("a retried save: %v", err)
	}
	price := int64(4_500_000)
	if err := r.SetDraft(domain.Draft{
		Title: "iPhone 11", CategoryCode: "phones.iphone", Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{{Label: "Pin", Value: "81%"}}, Price: &price, AllowOffers: true, Photos: []uuid.UUID{frame},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(&domain.Estimate{Low: 4_400_000, Median: 4_800_000, High: 5_100_000, Source: domain.SourceModel}); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.Status() != domain.StatusDone || got.Frames()[0].UploadID != frame || got.Draft().Specs[0].Value != "81%" ||
		*got.Draft().Price != price || got.Estimate().Source != domain.SourceModel || got.WriteID() != r.WriteID() {
		t.Fatalf("%+v %v", got.State(), err)
	}
	// insert, frames (the retry added none), draft, finish
	if s := seq(t, db, me); s != 4 {
		t.Fatalf("seq %d, want 4", s)
	}
	stale := retry
	if err := stale.Fail(domain.FailUnavailable); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save from an old copy: %v", err)
	}
}

func TestFailedSaveKeepsTheFailure(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewVideoReads(db)
	r := read(accounttest.New(t, db), now)
	if err := reads.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Fail(domain.FailNoItem); err != nil {
		t.Fatal(err)
	}
	if err := reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.Status() != domain.StatusFailed || got.Failure() != domain.FailNoItem {
		t.Fatalf("%+v %v", got.State(), err)
	}
}

// 29 reads in the window and one older: two reads started at once take the
// 30th place one at a time, so exactly one passes.
func TestInsertCapUnderRace(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewVideoReads(db)
	me := accounttest.New(t, db)
	if err := reads.Insert(ctx, read(me, now.Add(-25*time.Hour))); err != nil {
		t.Fatal(err)
	}
	for range domain.ReadLimit - 1 {
		if err := reads.Insert(ctx, read(me, now.Add(-time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = reads.Insert(ctx, read(me, now)) })
	}
	wg.Wait()
	passed, refused := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			passed++
		case errors.Is(err, domain.ErrReadLimit):
			refused++
		default:
			t.Fatalf("insert: %v", err)
		}
	}
	if passed != 1 || refused != 1 {
		t.Fatalf("passed %d refused %d", passed, refused)
	}
	// Another account has its own 30.
	if err := reads.Insert(ctx, read(accounttest.New(t, db), now)); err != nil {
		t.Fatal(err)
	}
}

func TestInsertRefusesADeletedAccount(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	gone := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewVideoReads(db).Insert(ctx, read(gone, now)); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("insert: %v", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewVideoReads(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	a, b, c := read(me, now), read(me, now), read(other, now)
	for _, r := range []domain.VideoRead{a, b, c} {
		if err := reads.Insert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := reads.Delete(ctx, a.ID()); err != nil {
		t.Fatal(err)
	}
	if err := reads.Delete(ctx, a.ID()); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := reads.Get(ctx, a.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted read: %v", err)
	}
	if err := reads.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	if _, err := reads.Get(ctx, b.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the account's read: %v", err)
	}
	if _, err := reads.Get(ctx, c.ID()); err != nil {
		t.Fatalf("another account's read: %v", err)
	}
}

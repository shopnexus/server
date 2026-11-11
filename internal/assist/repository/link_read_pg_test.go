package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
	"github.com/shopnexus/server2/internal/assist/repository"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

var listing = domain.Link{Site: domain.SiteChotot, ListID: 119482736, URL: "https://www.chotot.com/119482736.htm"}

func linkRead(account uuid.UUID, at time.Time) domain.LinkRead {
	return domain.NewLinkRead(uuid.NewV7(), account, listing, at)
}

func TestLinkReadInsertSaveGet(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewLinkReads(db)
	me := accounttest.New(t, db)
	r := linkRead(me, now)
	if err := reads.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := reads.Insert(ctx, r); err != nil {
		t.Fatalf("a retried insert: %v", err)
	}
	got, err := reads.Get(ctx, r.ID())
	if err != nil || got.Link() != listing || got.Status() != domain.StatusReading || got.PhotoCount() != 0 || got.Draft() != nil ||
		got.ContactRemoved() {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if err = r.SetPhotoCount(5); err != nil {
		t.Fatal(err)
	}
	retry := r
	first := uuid.NewV7()
	if err = reads.Save(ctx, &r, first); err != nil {
		t.Fatal(err)
	}
	if err = reads.Save(ctx, &retry, first); err != nil {
		t.Fatalf("a retried save: %v", err)
	}
	price, photo := int64(4_500_000), uuid.NewV7()
	if err = r.SetDraft(domain.Draft{
		Title: "iPhone 11", CategoryCode: "phones.iphone", Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}}, Price: &price, Photos: []uuid.UUID{photo},
	}, true); err != nil {
		t.Fatal(err)
	}
	if err = reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err = r.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if err = reads.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err = reads.Get(ctx, r.ID())
	if err != nil || got.Status() != domain.StatusDone || got.PhotoCount() != 5 || got.Draft().Photos[0] != photo || !got.ContactRemoved() ||
		got.Estimate() != nil || got.WriteID() != r.WriteID() {
		t.Fatalf("%+v %v", got.State(), err)
	}
	p, err := stream.PGLog{DB: db}.Since(ctx, me, 0, 10)
	if err != nil || len(p.Updates) != 4 || p.Updates[0].Kind != stream.KindLinkRead || *p.Updates[3].LinkReadID != r.ID() {
		t.Fatalf("insert, count, draft, done: %+v %v", p, err)
	}
	stale := retry
	if err = stale.Fail(domain.FailUnavailable); err != nil {
		t.Fatal(err)
	}
	if err = reads.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save from an old copy: %v", err)
	}
	if _, err = reads.Get(ctx, uuid.NewV7()); !errors.Is(err, domain.ErrLinkReadNotFound) {
		t.Fatalf("unknown read: %v", err)
	}
}

// Link reads have their own 30, apart from video reads.
func TestLinkReadCapUnderRace(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads, videos := repository.NewLinkReads(db), repository.NewVideoReads(db)
	me := accounttest.New(t, db)
	for range domain.ReadLimit {
		if err := videos.Insert(ctx, read(me, now.Add(-time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if err := reads.Insert(ctx, linkRead(me, now.Add(-25*time.Hour))); err != nil {
		t.Fatal(err)
	}
	for range domain.ReadLimit - 1 {
		if err := reads.Insert(ctx, linkRead(me, now.Add(-time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = reads.Insert(ctx, linkRead(me, now)) })
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
}

func TestLinkReadInsertRefusesADeletedAccount(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	gone := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewLinkReads(db).Insert(ctx, linkRead(gone, now)); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("insert: %v", err)
	}
}

func TestLinkReadDelete(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	reads := repository.NewLinkReads(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	a, b, c := linkRead(me, now), linkRead(me, now), linkRead(other, now)
	for _, r := range []domain.LinkRead{a, b, c} {
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
	if err := reads.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	if _, err := reads.Get(ctx, b.ID()); !errors.Is(err, domain.ErrLinkReadNotFound) {
		t.Fatalf("the account's read: %v", err)
	}
	if _, err := reads.Get(ctx, c.ID()); err != nil {
		t.Fatalf("another account's read: %v", err)
	}
}

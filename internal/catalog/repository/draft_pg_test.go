package repository_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func draftContent(title string, photos ...uuid.UUID) domain.DraftContent {
	return domain.DraftContent{SchemaVersion: 1, Title: title, Price: new(int64(1_500_000)), Photos: photos, Body: `{"condition":"used"}`}
}

// A create, then a replace of the same ID; the list reads both fields and
// times back.
func TestDraftPutAndList(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	id, photo := uuid.NewV7(), uuid.NewV7()
	d, created, err := s.Put(ctx, me, id, draftContent("Xe đạp", photo), now)
	if err != nil || !created || d.ID() != id || !d.ExpiresAt().Equal(now.Add(domain.DraftLife)) {
		t.Fatalf("create: %+v %v %v", d.State(), created, err)
	}
	later := now.Add(time.Hour)
	noPrice := domain.DraftContent{SchemaVersion: 2, Body: `{}`}
	if d, created, err = s.Put(ctx, me, id, noPrice, later); err != nil || created || !d.CreatedAt().Equal(now) {
		t.Fatalf("replace: %+v %v %v", d.State(), created, err)
	}
	other := uuid.NewV7()
	if _, _, err = s.Put(ctx, me, other, draftContent("Xe máy", photo), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, me, later)
	if err != nil || len(list) != 2 || list[0].ID() != id || list[1].ID() != other {
		t.Fatalf("list %d %v", len(list), err)
	}
	if c := list[0].Content(); c.Price != nil || c.SchemaVersion != 2 || c.Body != `{}` || len(c.Photos) != 0 ||
		!list[0].UpdatedAt().Equal(later) || !list[0].CreatedAt().Equal(now) {
		t.Fatalf("replaced: %+v", list[0].State())
	}
	if c := list[1].Content(); *c.Price != 1_500_000 || !slices.Equal(c.Photos, []uuid.UUID{photo}) || c.Body != `{"condition":"used"}` {
		t.Fatalf("other: %+v", list[1].State())
	}
}

// Another account's ID is draft_not_found, and its draft stays.
func TestDraftPutOnAnotherAccountsID(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	me, them := accounttest.New(t, db), accounttest.New(t, db)
	id := uuid.NewV7()
	if _, _, err := s.Put(ctx, them, id, draftContent("Của họ"), now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put(ctx, me, id, draftContent("Của tôi"), now); !errors.Is(err, domain.ErrDraftNotFound) {
		t.Fatalf("%v", err)
	}
	if list, _ := s.List(ctx, them, now); len(list) != 1 || list[0].Content().Title != "Của họ" {
		t.Fatalf("their draft: %d", len(list))
	}
}

// Two creates at 19 drafts wait for each other: one lands, the other finds
// the limit. A replace at 20 still lands.
func TestTwoCreatesAtNineteen(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	first := uuid.NewV7()
	for i := range domain.MaxDrafts - 1 {
		id := uuid.NewV7()
		if i == 0 {
			id = first
		}
		if _, _, err := s.Put(ctx, me, id, draftContent(""), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, _, errs[i] = s.Put(ctx, me, uuid.NewV7(), draftContent(""), now.Add(time.Hour)) })
	}
	wg.Wait()
	landed, limited := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			landed++
		case errors.Is(err, domain.ErrDraftLimitReached):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if list, _ := s.List(ctx, me, now.Add(time.Hour)); landed != 1 || limited != 1 || len(list) != domain.MaxDrafts {
		t.Fatalf("%d landed, %d limited, %d drafts", landed, limited, len(list))
	}
	if _, created, err := s.Put(ctx, me, first, draftContent("edited"), now.Add(2*time.Hour)); err != nil || created {
		t.Fatalf("a replace at 20: %v %v", created, err)
	}
}

// Expired drafts go before a list and before a put, so they free their
// places; another account's stay.
func TestExpiredDraftsGo(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	me, them := accounttest.New(t, db), accounttest.New(t, db)
	for range domain.MaxDrafts {
		if _, _, err := s.Put(ctx, me, uuid.NewV7(), draftContent(""), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Put(ctx, them, uuid.NewV7(), draftContent(""), now); err != nil {
		t.Fatal(err)
	}
	end := now.Add(domain.DraftLife)
	if _, _, err := s.Put(ctx, me, uuid.NewV7(), draftContent("21st"), end); err != nil {
		t.Fatalf("a create once the 20 expired: %v", err)
	}
	if list, _ := s.List(ctx, me, end); len(list) != 1 || list[0].Content().Title != "21st" {
		t.Fatalf("%d drafts", len(list))
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listing_drafts WHERE account_id = @a`, pgx.StrictNamedArgs{"a": them}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("their draft: %d %v", n, err)
	}
	if list, _ := s.List(ctx, them, end); len(list) != 0 {
		t.Fatal("an expired draft was listed")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listing_drafts WHERE account_id = @a`, pgx.StrictNamedArgs{"a": them}).Scan(&n); err != nil || n != 0 {
		t.Fatalf("their expired draft after their list: %d %v", n, err)
	}
}

// A draft write that commits after the account's erasure finds the
// tombstone and writes nothing.
func TestDraftPutRefusesATombstone(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	gone := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put(ctx, gone, uuid.NewV7(), draftContent(""), now); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("put: %v", err)
	}
	if list, _ := s.List(ctx, gone, now); len(list) != 0 {
		t.Fatalf("%d drafts", len(list))
	}
}

// Delete touches only the caller's draft; DeleteAll all of theirs.
func TestDraftDelete(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDrafts(db)
	ctx := context.Background()
	me, them := accounttest.New(t, db), accounttest.New(t, db)
	mine, theirs := uuid.NewV7(), uuid.NewV7()
	for _, p := range []struct{ account, id uuid.UUID }{{me, mine}, {me, uuid.NewV7()}, {them, theirs}} {
		if _, _, err := s.Put(ctx, p.account, p.id, draftContent(""), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(ctx, me, theirs); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Delete(ctx, me, mine); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := s.List(ctx, me, now); len(list) != 1 {
		t.Fatalf("mine: %d", len(list))
	}
	if list, _ := s.List(ctx, them, now); len(list) != 1 {
		t.Fatalf("theirs: %d", len(list))
	}
	if err := s.DeleteAll(ctx, me); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, me, now); len(list) != 0 {
		t.Fatalf("after DeleteAll: %d", len(list))
	}
}

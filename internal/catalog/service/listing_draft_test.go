package service_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/domain"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
)

// kept reports whether a PUT kept the upload for about 30 days.
func kept(at time.Time) bool { return at.After(time.Now().Add(29 * 24 * time.Hour)) }

// A create keeps its photos 30 days; a replace keeps the new ones and
// leaves a dropped one to its own expiry; the list shows what is stored,
// with a URL per photo.
//
// UC-LST-01 A4.
func TestPutDraftCreatesAndReplaces(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.account(t)
	p, q := e.photo(t, me), e.photo(t, me)
	id := uuid.NewV7()
	v, created, err := e.catalog.PutDraft(ctx, me, id, content("iPhone 13", p, q, p))
	if err != nil || !created || !slices.Equal(photoIDs(v), []uuid.UUID{p, q}) || v.Photos[0].URL == "" || v.Body != `{"condition":"like_new"}` {
		t.Fatalf("create: %+v %v %v", v, created, err)
	}
	if !kept(e.expiresAt(t, p)) || !kept(e.expiresAt(t, q)) {
		t.Fatal("the photos were not kept")
	}
	dropped := e.expiresAt(t, p)
	v, created, err = e.catalog.PutDraft(ctx, me, id, content("iPhone 13 Pro", q))
	if err != nil || created || v.Title != "iPhone 13 Pro" || !slices.Equal(photoIDs(v), []uuid.UUID{q}) {
		t.Fatalf("replace: %+v %v %v", v, created, err)
	}
	if got := e.expiresAt(t, p); !got.Equal(dropped) {
		t.Fatalf("the dropped photo's expiry moved to %v", got)
	}
	list, err := e.catalog.ListDrafts(ctx, me)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].Title != "iPhone 13 Pro" || len(list[0].Photos) != 1 || list[0].Photos[0].URL == "" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestPutDraftRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.account(t), e.account(t)
	theirID := uuid.NewV7()
	if _, _, err := e.catalog.PutDraft(ctx, other, theirID, content("Của họ")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.catalog.PutDraft(ctx, me, theirID, content("")); !errors.Is(err, domain.ErrDraftNotFound) {
		t.Fatalf("another account's ID: %v", err)
	}

	mine, theirs := e.photo(t, me), e.photo(t, other)
	before := e.expiresAt(t, mine)
	if _, _, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("", mine, theirs)); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("their photo: %v", err)
	}
	if got := e.expiresAt(t, mine); !got.Equal(before) {
		t.Fatalf("a refused PUT kept a photo until %v", got)
	}
	if list, _ := e.catalog.ListDrafts(ctx, me); len(list) != 0 {
		t.Fatalf("a refused PUT wrote %d drafts", len(list))
	}

	first := uuid.NewV7()
	for i := range domain.MaxDrafts {
		id := uuid.NewV7()
		if i == 0 {
			id = first
		}
		if _, _, err := e.catalog.PutDraft(ctx, me, id, content("")); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("21st")); !errors.Is(err, domain.ErrDraftLimitReached) {
		t.Fatalf("the 21st: %v", err)
	}
	if _, created, err := e.catalog.PutDraft(ctx, me, first, content("edited at 20")); err != nil || created {
		t.Fatalf("a replace at the limit: %v %v", created, err)
	}
}

// A seller at 20 drafts, all past their 30 days, creates one: the expired
// ones go first and free their places. The same goes for the draft the
// phone sends again after the server let it expire: it comes back.
func TestExpiredDraftsFreeTheirPlaces(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.account(t)
	old := time.Now().Add(-domain.DraftLife - time.Hour)
	revived := uuid.NewV7()
	for i := range domain.MaxDrafts {
		id := uuid.NewV7()
		if i == 0 {
			id = revived
		}
		if _, _, err := e.drafts.Put(ctx, me, id, content(""), old); err != nil {
			t.Fatal(err)
		}
	}
	if _, created, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("new")); err != nil || !created {
		t.Fatalf("a create at 20 expired drafts: %v %v", created, err)
	}
	if _, created, err := e.catalog.PutDraft(ctx, me, revived, content("back")); err != nil || !created {
		t.Fatalf("an expired draft sent again: %v %v", created, err)
	}
	list, err := e.catalog.ListDrafts(ctx, me)
	if err != nil || len(list) != 2 || list[0].ID != revived {
		t.Fatalf("%d drafts %v", len(list), err)
	}
}

// A photo a listing took, one whose 24 hours ran out while the phone was
// offline (MediaExpire has not run yet), and one with no upload are left
// out of what is stored; the rest syncs.
func TestPutDraftDropsGonePhotos(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.account(t)
	taken, stale, good := e.photo(t, me), e.photo(t, me), e.photo(t, me)
	if err := e.media.Attach(ctx, me, uuid.NewV7(), []uuid.UUID{taken}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE uploads SET expires_at = now() - interval '1 hour' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": stale}); err != nil {
		t.Fatal(err)
	}
	v, _, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("", taken, stale, good, uuid.NewV7()))
	if err != nil || !slices.Equal(photoIDs(v), []uuid.UUID{good}) {
		t.Fatalf("%+v %v", v, err)
	}
	if list, _ := e.drafts.List(ctx, me, time.Now()); len(list) != 1 || !slices.Equal(list[0].Photos(), []uuid.UUID{good}) {
		t.Fatalf("stored %+v", list)
	}
}

// A photo a listing took after the PUT leaves the list's answer without a
// write to the draft.
func TestListDraftsLeavesTakenPhotosOut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.account(t)
	taken, left := e.photo(t, me), e.photo(t, me)
	put, _, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("", taken, left))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.media.Attach(ctx, me, uuid.NewV7(), []uuid.UUID{taken}); err != nil {
		t.Fatal(err)
	}
	list, err := e.catalog.ListDrafts(ctx, me)
	if err != nil || len(list) != 1 || !slices.Equal(photoIDs(list[0]), []uuid.UUID{left}) || !strings.Contains(list[0].Photos[0].URL, left.String()) ||
		list[0].UpdatedAt.Sub(put.UpdatedAt).Abs() > time.Millisecond { // no write: the time is the PUT's, to the microsecond
		t.Fatalf("%+v %v", list, err)
	}
}

// Delete is quiet about a draft that is gone or another account's, and
// leaves the photos to their own expiry.
//
// UC-LST-01 A4.
func TestDeleteDraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.account(t), e.account(t)
	id, p := uuid.NewV7(), e.photo(t, me)
	if _, _, err := e.catalog.PutDraft(ctx, me, id, content("", p)); err != nil {
		t.Fatal(err)
	}
	if err := e.catalog.DeleteDraft(ctx, other, id); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.catalog.ListDrafts(ctx, me); len(list) != 1 {
		t.Fatal("another account deleted it")
	}
	thirty := e.expiresAt(t, p)
	for range 2 {
		if err := e.catalog.DeleteDraft(ctx, me, id); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := e.catalog.ListDrafts(ctx, me); len(list) != 0 {
		t.Fatal("still there")
	}
	if got := e.expiresAt(t, p); !got.Equal(thirty) {
		t.Fatalf("the photo's expiry moved to %v", got)
	}
}

// A PUT after the account's erasure writes nothing; the erasure's
// SellerLeft deletes the drafts, a second run finding none.
func TestDraftsAndErasure(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, gone := e.account(t), e.account(t)
	for range 2 {
		if _, _, err := e.catalog.PutDraft(ctx, me, uuid.NewV7(), content("")); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := e.catalog.SellerLeft(ctx, me); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := e.drafts.List(ctx, me, time.Now()); len(list) != 0 {
		t.Fatalf("%d drafts after erasure", len(list))
	}
	if _, err := e.db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.catalog.PutDraft(ctx, gone, uuid.NewV7(), content("")); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("a deleted account's put: %v", err)
	}
}

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// same compares stored states, times by instant: Postgres returns them in
// the session's time zone.
func same(a, b domain.UploadState) bool {
	return a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) &&
		a.ID == b.ID && a.OwnerID == b.OwnerID && a.ContentType == b.ContentType && a.Size == b.Size &&
		a.Status == b.Status && a.PhotoKey == b.PhotoKey && a.ThumbKey == b.ThumbKey && a.Failure == b.Failure &&
		a.Shape == b.Shape && a.WriteID == b.WriteID
}

func created(t *testing.T, s repository.Uploads, owner uuid.UUID) domain.Upload {
	t.Helper()
	u, err := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 100, domain.OriginApp, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateGetSave(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewUploads(db)
	ctx := context.Background()
	owner, holder := accounttest.New(t, db), uuid.NewV7()
	u := created(t, s, owner)
	got, err := s.Get(ctx, u.ID())
	if err != nil || !same(got.State(), u.State()) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	// Save leaves the upload carrying the write ID it wrote, so saving it
	// again succeeds.
	_, _ = got.Attach(owner, holder, domain.ShapeSquare, now)
	if err := s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got.Processed("media/x.jpg", "media/x_thumb.jpg", now)
	if err := s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if again, _ := s.Get(ctx, u.ID()); again.HolderID() != holder || again.Shape() != domain.ShapeSquare ||
		again.Status() != domain.StatusReady {
		t.Fatalf("holder not stored: %+v", again.State())
	}
	stale := u
	if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
}

// GetMany returns what it finds; SaveAll writes all or none.
func TestGetManyAndSaveAll(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewUploads(db)
	ctx := context.Background()
	owner, holder := accounttest.New(t, db), uuid.NewV7()
	a, b := created(t, s, owner), created(t, s, owner)
	got, err := s.GetMany(ctx, []uuid.UUID{a.ID(), b.ID(), uuid.NewV7()})
	if err != nil || len(got) != 2 {
		t.Fatalf("%d %v", len(got), err)
	}
	stale := got[1]
	for i := range got {
		_, _ = got[i].Attach(owner, holder, domain.ShapePhoto, now)
	}
	staleCopy := stale
	_, _ = staleCopy.Attach(owner, holder, domain.ShapePhoto, now)
	if err := s.Save(ctx, &stale, uuid.NewV7()); err != nil { // moves b's write ID
		t.Fatal(err)
	}
	ups := []*domain.Upload{&got[0], &got[1]}
	if err := s.SaveAll(ctx, ups, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("one stale: %v", err)
	}
	if first, _ := s.Get(ctx, a.ID()); first.Status() != domain.StatusWaiting {
		t.Fatalf("a was saved without b: %s", first.Status())
	}
}

// The table takes a 50 MB video, held in shape video, and its consumed
// status.
func TestStoreAVideo(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewUploads(db)
	ctx := context.Background()
	owner := accounttest.New(t, db)
	u, err := domain.NewUpload(uuid.NewV7(), owner, domain.ContentTypeMP4, domain.MaxVideoBytes, domain.OriginApp, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Attach(owner, uuid.NewV7(), domain.ShapeVideo, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &u, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, u.ID())
	if err != nil || !same(got.State(), u.State()) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if _, err = db.Exec(ctx, `UPDATE uploads SET status = 'consumed' WHERE id = $1`, u.ID()); err != nil {
		t.Fatalf("consumed: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE uploads SET size = 15728641, content_type = 'image/jpeg', shape = 'photo' WHERE id = $1`,
		u.ID()); err == nil {
		t.Fatal("a photo over 15 MB was stored")
	}
}

// A server still on the code before 0037 inserts uploads without an
// origin while the new one rolls out; those rows are the app's.
func TestInsertWithoutOriginIsTheApps(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	owner, id := accounttest.New(t, db), uuid.NewV7()
	if _, err := db.Exec(ctx, `
INSERT INTO uploads (id, owner_id, content_type, size, status, write_id, created_at, updated_at)
VALUES (@id, @owner, 'image/jpeg', 100, 'waiting', @id, now(), now())`, pgx.StrictNamedArgs{"id": id, "owner": owner}); err != nil {
		t.Fatalf("an insert without origin: %v", err)
	}
	u, err := repository.NewUploads(db).Get(ctx, id)
	if err != nil || u.Origin() != domain.OriginApp {
		t.Fatalf("%+v %v", u.State(), err)
	}
}

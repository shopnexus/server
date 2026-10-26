package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// same compares stored states, times by instant: Postgres returns them in
// the session's time zone.
func same(a, b domain.UploadState) bool {
	return a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) &&
		a.ID == b.ID && a.OwnerID == b.OwnerID && a.ContentType == b.ContentType && a.Size == b.Size &&
		a.Status == b.Status && a.PhotoKey == b.PhotoKey && a.ThumbKey == b.ThumbKey && a.Failure == b.Failure &&
		a.WriteID == b.WriteID
}

func created(t *testing.T, s repository.Uploads, owner uuid.UUID) domain.Upload {
	t.Helper()
	u, err := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 100, now)
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
	owner, holder := newAccount(t, db), uuid.NewV7()
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
	_, _ = got.Attach(owner, holder, now)
	if err := s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got.Processed("media/x.jpg", "media/x_thumb.jpg", now)
	if err := s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if again, _ := s.Get(ctx, u.ID()); again.HolderID() != holder || again.Status() != domain.StatusReady {
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
	owner, holder := newAccount(t, db), uuid.NewV7()
	a, b := created(t, s, owner), created(t, s, owner)
	got, err := s.GetMany(ctx, []uuid.UUID{a.ID(), b.ID(), uuid.NewV7()})
	if err != nil || len(got) != 2 {
		t.Fatalf("%d %v", len(got), err)
	}
	stale := got[1]
	for i := range got {
		_, _ = got[i].Attach(owner, holder, now)
	}
	staleCopy := stale
	_, _ = staleCopy.Attach(owner, holder, now)
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

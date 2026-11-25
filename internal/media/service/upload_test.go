package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

func TestCreateUploadIsWaitingWithAPresignedPut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner := accounttest.New(t, e.db)
	ticket, err := e.media.CreateUpload(context.Background(), owner, "image/png", 12)
	if err != nil || ticket.Headers["Content-Type"] != "image/png" || ticket.ExpiresAt.Before(time.Now()) {
		t.Fatalf("%+v %v", ticket, err)
	}
	u, err := repository.NewUploads(e.db).Get(context.Background(), ticket.UploadID)
	if err != nil || u.Status() != domain.StatusWaiting || u.OwnerID() != owner {
		t.Fatalf("%+v %v", u.State(), err)
	}
	if _, err := e.media.CreateUpload(context.Background(), owner, "image/heic", 12); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("heic: %v", err)
	}
}

func TestAttachRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, other, holder := accounttest.New(t, e.db), accounttest.New(t, e.db), uuid.NewV7()
	mine := e.upload(t, owner, []byte("jpeg bytes"))
	theirs := e.upload(t, other, []byte("jpeg bytes"))
	notPut, err := e.media.CreateUpload(ctx, owner, "image/jpeg", 10)
	if err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]uuid.UUID{
		"another owner's": {mine, theirs},
		"never put":       {mine, notPut.UploadID},
		"unknown":         {mine, uuid.NewV7()},
	} {
		if err := e.media.Attach(ctx, owner, holder, ids, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if u, _ := repository.NewUploads(e.db).Get(ctx, mine); u.Status() != domain.StatusWaiting {
		t.Fatalf("a refused attach attached %s", u.Status())
	}
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{mine}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{mine}, uuid.NewV7()); err != nil {
		t.Fatalf("a retried attach: %v", err)
	}
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{mine}, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("attached to another holder: %v", err)
	}
}

func TestExpireDeletesAWaitingUpload(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	id := e.upload(t, accounttest.New(t, e.db), []byte("jpeg bytes"))
	e.expire(t, id)
	u, _ := repository.NewUploads(e.db).Get(ctx, id)
	if u.Status() != domain.StatusExpired {
		t.Fatalf("status %s", u.Status())
	}
	if _, _, err := e.store.Stat(ctx, u.Key()); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("object: %v", err)
	}
}

func TestExpireLeavesAttachedUploads(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{id}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	e.expire(t, id)
	u, _ := repository.NewUploads(e.db).Get(ctx, id)
	if u.Status() != domain.StatusAttached {
		t.Fatalf("status %s", u.Status())
	}
	if _, _, err := e.store.Stat(ctx, u.Key()); err != nil {
		t.Fatalf("object: %v", err)
	}
	e.expire(t, uuid.NewV7()) // an unknown upload is nothing to do
}

// A video is an upload like a photo, and nothing takes it as a photo: not a
// listing, not an avatar, not a search.
func TestVideoIsNoPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	ticket, err := e.media.CreateUpload(ctx, owner, domain.ContentTypeMP4, domain.MaxVideoBytes)
	if err != nil || ticket.Headers["Content-Type"] != domain.ContentTypeMP4 {
		t.Fatalf("a 50 MB video: %+v %v", ticket, err)
	}
	if _, err = e.media.CreateUpload(ctx, owner, "image/jpeg", domain.MaxPhotoBytes+1); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a photo over 15 MB: %v", err)
	}
	video := mediatest.Upload(t, e.media, owner, domain.ContentTypeQuickTime, []byte("not really a mov"))
	if err = e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{video}, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a video as a listing photo: %v", err)
	}
	if _, err = e.media.Square(ctx, owner, owner, video); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a video as an avatar: %v", err)
	}
	if _, err = e.media.QueryUpload(ctx, owner, video); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a video as a search photo: %v", err)
	}
	if _, err = e.media.QueryImage(ctx, owner, video); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a video read as a search photo: %v", err)
	}
	if u, _ := repository.NewUploads(e.db).Get(ctx, video); u.Status() != domain.StatusWaiting {
		t.Fatalf("a refused video changed: %s", u.Status())
	}
}

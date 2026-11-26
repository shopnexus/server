package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// Extend takes all of the owner's photos or none; one that is another
// account's, unknown, never put or a video leaves the rest as they were.
// Extending again moves the expiry out, never in.
//
// UC-SHR-01 A1.
func TestExtendAllOrNone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	mine := e.upload(t, owner, []byte("jpeg bytes"))
	theirs := e.upload(t, accounttest.New(t, e.db), []byte("jpeg bytes"))
	never, err := e.media.CreateUpload(ctx, owner, "image/jpeg", 10)
	if err != nil {
		t.Fatal(err)
	}
	video, err := e.media.CreateUpload(ctx, owner, domain.ContentTypeMP4, 10)
	if err != nil {
		t.Fatal(err)
	}
	first := e.get(t, mine).ExpiresAt()
	for name, ids := range map[string][]uuid.UUID{
		"another account's": {mine, theirs},
		"unknown":           {mine, uuid.NewV7()},
		"never put":         {mine, never.UploadID},
		"a video":           {mine, video.UploadID},
	} {
		if err := e.media.Extend(ctx, owner, ids); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := e.get(t, mine).ExpiresAt(); !got.Equal(first) {
		t.Fatalf("extended by a refusal: %v", got)
	}
	if err := e.media.Extend(ctx, owner, []uuid.UUID{mine, mine}); err != nil {
		t.Fatal(err)
	}
	if u := e.get(t, mine); u.Status() != domain.StatusWaiting || u.ExpiresAt().Sub(e.clock.Now().Add(domain.ExtendFor)).Abs() > time.Millisecond {
		t.Fatalf("%s, expires %v", u.Status(), u.ExpiresAt())
	}
	if err := e.media.Extend(ctx, owner, nil); err != nil {
		t.Fatalf("nothing to extend: %v", err)
	}
}

// An expired upload is not extended.
func TestExtendRefusesAnExpiredPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	e.expire(t, id)
	if err := e.media.Extend(ctx, owner, []uuid.UUID{id}); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("%v", err)
	}
}

// MediaExpire, waking a day after the upload, finds it extended and sends
// itself again; at the extended expiry it deletes the upload and its
// object.
func TestExtendedPhotoWaitsForItsExpiry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	if err := e.media.Extend(ctx, owner, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	e.expire(t, id)
	if s := e.get(t, id).Status(); s != domain.StatusWaiting {
		t.Fatalf("a day after the upload: %s", s)
	}
	if _, _, err := e.store.Stat(ctx, domain.UploadKey(id)); err != nil {
		t.Fatalf("object: %v", err)
	}
	e.clock.Add(domain.ExtendFor - domain.ExpireAfter)
	e.runExpire(t, id)
	if s := e.get(t, id).Status(); s != domain.StatusExpired {
		t.Fatalf("30 days after the extension: %s", s)
	}
	if _, _, err := e.store.Stat(ctx, domain.UploadKey(id)); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("object: %v", err)
	}
}

// A photo two PUTs extended, ten days apart, lives until the later
// extension's end: the run sent for the first one finds the expiry moved
// and sends itself again.
func TestExtendedTwiceWaitsForTheLater(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	if err := e.media.Extend(ctx, owner, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(10 * 24 * time.Hour)
	if err := e.media.Extend(ctx, owner, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(domain.ExtendFor - 10*24*time.Hour) // the first extension's end
	e.runExpire(t, id)
	if s := e.get(t, id).Status(); s != domain.StatusWaiting {
		t.Fatalf("at the first extension's end: %s", s)
	}
	e.clock.Add(10 * 24 * time.Hour)
	e.runExpire(t, id)
	if s := e.get(t, id).Status(); s != domain.StatusExpired {
		t.Fatalf("at the second's: %s", s)
	}
}

// Gone names the owner's uploads a listing took, those that expired and
// the IDs with no upload; a waiting or extended one, or another account's,
// is not gone.
func TestGoneUploads(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	expired := e.upload(t, owner, []byte("jpeg bytes"))
	e.expire(t, expired)
	waiting := e.upload(t, owner, []byte("jpeg bytes"))
	extended := e.upload(t, owner, []byte("jpeg bytes"))
	attached := e.upload(t, owner, []byte("jpeg bytes"))
	theirs := e.upload(t, accounttest.New(t, e.db), []byte("jpeg bytes"))
	unknown := uuid.NewV7()
	if err := e.media.Extend(ctx, owner, []uuid.UUID{extended}); err != nil {
		t.Fatal(err)
	}
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{attached}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	gone, err := e.media.Gone(ctx, owner, []uuid.UUID{waiting, extended, attached, expired, theirs, unknown})
	if err != nil || !slices.Equal(gone, []uuid.UUID{attached, expired, unknown}) {
		t.Fatalf("gone %v %v", gone, err)
	}
}

// A listing takes an extended photo, two days after its first day ended.
func TestAttachAnExtendedPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	if err := e.media.Extend(ctx, owner, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(3 * domain.ExpireAfter)
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{id}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if s := e.get(t, id).Status(); s != domain.StatusAttached {
		t.Fatalf("status %s", s)
	}
}

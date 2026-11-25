package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// A holder's release deletes an unprocessed photo's original at once and
// a processed one's public copies at the purge; another holder's upload,
// and one the holder still shows, stay.
func TestReleaseDeletesTheHoldersObjects(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, holder := accounttest.New(t, e.db), uuid.NewV7()
	pending, done, kept := e.upload(t, owner, mediatest.JPEG(t, 64, 48)), e.upload(t, owner, mediatest.JPEG(t, 64, 48)),
		e.upload(t, owner, mediatest.JPEG(t, 64, 48))
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{pending, done, kept}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	other := e.uploadAs(t, "image/jpeg", mediatest.JPEG(t, 64, 48))
	if r := e.process(t, done); r.Status != domain.StatusReady {
		t.Fatalf("%+v", r)
	}
	in := service.ReleaseInput{Holder: holder, IDs: []uuid.UUID{pending, done, other}}
	for range 2 {
		if _, err := ingress.Service[service.ReleaseInput, restate.Void](e.ingress, e.prefix+"MediaRelease", "Run").Request(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[uuid.UUID]domain.Status{
		pending: domain.StatusReleased, done: domain.StatusReleased,
		kept: domain.StatusAttached, other: domain.StatusAttached,
	} {
		if got := e.get(t, id).Status(); got != want {
			t.Errorf("upload %s: %s, want %s", id, got, want)
		}
	}
	if _, _, err := e.store.Stat(ctx, domain.UploadKey(pending)); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the released original is still there: %v", err)
	}
	if _, _, err := e.store.Stat(ctx, domain.UploadKey(kept)); err != nil {
		t.Fatalf("a shown upload lost its original: %v", err)
	}
	photo, thumb := domain.PublicKeys(done)
	if _, _, err := e.store.Stat(ctx, photo); err != nil {
		t.Fatalf("the photo went before the purge: %v", err)
	}
	if r := e.process(t, pending); r.Status != domain.StatusFailed {
		t.Fatalf("a released upload processed: %+v", r)
	}

	for _, id := range []uuid.UUID{done, other} {
		if _, err := ingress.Service[uuid.UUID, restate.Void](e.ingress, e.prefix+"MediaRelease", "Purge").Request(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{photo, thumb} {
		if _, _, err := e.store.Stat(ctx, key); !errors.Is(err, objectstore.ErrNotFound) {
			t.Errorf("%s is still there: %v", key, err)
		}
	}
	if _, _, err := e.store.Stat(ctx, domain.UploadKey(other)); err != nil {
		t.Fatalf("a purge of an upload still held deleted it: %v", err)
	}
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{done}, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a released upload attached again: %v", err)
	}
}

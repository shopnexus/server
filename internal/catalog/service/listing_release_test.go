package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// uploadsAre waits until each upload has its status.
func (e env) uploadsAre(t *testing.T, want map[uuid.UUID]mediadomain.Status) {
	t.Helper()
	testenv.Eventually(t, 30*time.Second, func() error {
		for id, status := range want {
			u, err := mediarepo.NewUploads(e.db).Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if u.Status() != status {
				return fmt.Errorf("upload %s is %s, want %s", id, u.Status(), status)
			}
		}
		return nil
	})
}

// onSale posts a listing with photos and waits until it is on sale.
func (e env) onSale(t *testing.T, seller uuid.UUID, photos ...uuid.UUID) uuid.UUID {
	t.Helper()
	v, err := e.catalog.Post(context.Background(), seller, uuid.NewV7(), draft(photos...), locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	if v = e.settle(t, seller, v.ID); v.Status != domain.StatusActive {
		t.Fatalf("%+v", v)
	}
	return v.ID
}

// An edit that drops a photo has media release it; the others stay.
func TestEditReleasesDroppedPhotos(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := e.seller(t)
	kept, dropped := e.photo(t, seller), e.photo(t, seller)
	id := e.onSale(t, seller, kept, dropped)
	if _, err := e.catalog.Edit(context.Background(), seller, id, draft(kept), locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	e.uploadsAre(t, map[uuid.UUID]mediadomain.Status{dropped: mediadomain.StatusReleased, kept: mediadomain.StatusReady})
}

// Tidy releases the uploads an edit attached and lost, but not those the
// listing shows.
func TestTidyReleasesLostAttaches(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	shown := e.photo(t, seller)
	id := e.onSale(t, seller, shown)
	lost := e.photo(t, seller)
	if err := e.media.Attach(ctx, seller, id, []uuid.UUID{lost}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.Service[service.TidyInput, restate.Void](e.ingress, e.prefix+"ListingEdit", "Tidy").
		Request(ctx, service.TidyInput{ID: id, Uploads: []uuid.UUID{shown, lost}}); err != nil {
		t.Fatal(err)
	}
	e.uploadsAre(t, map[uuid.UUID]mediadomain.Status{lost: mediadomain.StatusReleased, shown: mediadomain.StatusReady})
}

// A deleted listing gives up every photo but its processed cover, which
// its orders still show.
func TestDeletedListingReleasesAllButTheCover(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	cover, other := e.photo(t, seller), e.photo(t, seller)
	id := e.onSale(t, seller, cover, other)
	if err := e.catalog.Delete(ctx, seller, id); err != nil {
		t.Fatal(err)
	}
	// The worker hands these to media's Release (media_release_test covers it).
	ids, err := e.catalog.DeletedUploads(ctx, id)
	if err != nil || len(ids) != 1 || ids[0] != other {
		t.Fatalf("released %v, want [%v]: %v", ids, other, err)
	}
}

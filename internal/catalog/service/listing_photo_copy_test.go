package service_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// The copies of a sold listing's photos are new waiting uploads of the
// seller's, in the listing's order, which a new listing takes and
// processes; the sold listing keeps its own photos, and a photo first
// copied from another site stays marked on the new listing.
//
// UC-LST-01 A5.
func TestCopyPhotosStartsANewListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	linked := uuid.NewV7()
	if err := e.media.SavePhoto(ctx, seller, linked, mediatest.JPEG(t, 64, 48), "image/jpeg", mediadomain.OriginLink); err != nil {
		t.Fatal(err)
	}
	posted, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(e.photo(t, seller), linked), locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	old := e.settle(t, seller, posted.ID)
	if _, err = e.catalog.MarkSold(ctx, seller, old.ID, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}

	copies, err := e.catalog.CopyPhotos(ctx, seller, old.ID, uuid.NewV7().String())
	if err != nil || len(copies) != 2 {
		t.Fatalf("%+v %v", copies, err)
	}
	uploads := mediarepo.NewUploads(e.db)
	for i, c := range copies {
		if c.UploadID == old.Photos[0].UploadID || c.UploadID == old.Photos[1].UploadID {
			t.Fatalf("copy %d reuses an old upload", i)
		}
		u, getErr := uploads.Get(ctx, c.UploadID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		want := []mediadomain.Origin{mediadomain.OriginListing, mediadomain.OriginLink}[i]
		if u.OwnerID() != seller || u.Status() != mediadomain.StatusWaiting || u.Origin() != want || u.ContentType() != "image/jpeg" {
			t.Fatalf("copy %d: owner %s, status %s, origin %s, type %s", i, u.OwnerID(), u.Status(), u.Origin(), u.ContentType())
		}
		res, getErr := http.Get(c.URL) //nolint:noctx // A test reads a presigned URL.
		if getErr != nil {
			t.Fatal(getErr)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("copy %d reads %d", i, res.StatusCode)
		}
	}

	again, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(copies[0].UploadID, copies[1].UploadID), locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	l := e.settle(t, seller, again.ID)
	if l.Status != domain.StatusActive || len(l.Photos) != 2 || l.Photos[0].Copied || !l.Photos[1].Copied || l.Photos[0].URL == "" {
		t.Fatalf("the new listing: %+v", l)
	}
	kept, err := e.catalog.Get(ctx, &seller, old.ID, locale.Vietnamese)
	if err != nil || kept.Status != domain.StatusSold || len(kept.Photos) != 2 || kept.Photos[0].URL != old.Photos[0].URL {
		t.Fatalf("the old listing: %+v %v", kept, err)
	}
}

// Only the seller copies, only a listing not deleted, and only the photos
// processed. A repeated key returns the same uploads; a new key makes new
// ones.
//
// UC-LST-01 A5.
func TestCopyPhotosRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, other := e.seller(t), e.seller(t)
	l := e.active(t, seller)
	if _, err := e.catalog.CopyPhotos(ctx, other, l.ID, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another seller: %v", err)
	}
	if _, err := e.catalog.CopyPhotos(ctx, seller, uuid.NewV7(), uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an unknown listing: %v", err)
	}
	key := uuid.NewV7().String()
	first, err := e.catalog.CopyPhotos(ctx, seller, l.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.catalog.CopyPhotos(ctx, seller, l.ID, key)
	if err != nil || len(first) != 1 || len(again) != 1 || again[0].UploadID != first[0].UploadID {
		t.Fatalf("the same key: %+v %+v %v", first, again, err)
	}
	fresh, err := e.catalog.CopyPhotos(ctx, seller, l.ID, uuid.NewV7().String())
	if err != nil || len(fresh) != 1 || fresh[0].UploadID == first[0].UploadID {
		t.Fatalf("a new key: %+v %+v %v", first, fresh, err)
	}

	// A listing still processing has no photo to copy yet.
	processing, err := domain.NewListing(uuid.NewV7(), seller, draft(uuid.NewV7()), domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Insert(ctx, processing); err != nil {
		t.Fatal(err)
	}
	if got, copyErr := e.catalog.CopyPhotos(ctx, seller, processing.ID(), uuid.NewV7().String()); copyErr != nil || len(got) != 0 {
		t.Fatalf("a listing in processing: %+v %v", got, copyErr)
	}

	if err = e.catalog.Delete(ctx, seller, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.catalog.CopyPhotos(ctx, seller, l.ID, uuid.NewV7().String()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a deleted listing: %v", err)
	}
}

// saveThenFail makes the first SavePhoto commit and then fail, as when the
// process dies before Restate journals the step.
type saveThenFail struct {
	mediasvc.Service
	restatetest.CommitThenFail
}

func (s *saveThenFail) SavePhoto(ctx context.Context, owner, id uuid.UUID, data []byte, contentType string, origin mediadomain.Origin) error {
	return s.After(s.Service.SavePhoto(ctx, owner, id, data, contentType, origin))
}

// A copy step that committed and then failed runs again with the same
// upload ID, so the seller gets one upload per photo.
//
// UC-LST-01 A5 · NFR-DUR-01.
func TestCopyPhotosRetriedStepSavesOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.Media = &saveThenFail{Service: d.Media} })
	ctx := context.Background()
	seller := e.seller(t)
	posted, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(e.photo(t, seller), e.photo(t, seller)), locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	l := e.settle(t, seller, posted.ID)
	copies, err := e.catalog.CopyPhotos(ctx, seller, l.ID, uuid.NewV7().String())
	if err != nil || len(copies) != 2 {
		t.Fatalf("%+v %v", copies, err)
	}
	var n int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM uploads WHERE owner_id = @seller AND origin = 'listing'`,
		pgx.StrictNamedArgs{"seller": seller}).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d copies stored, want 2: %v", n, err)
	}
}

// OnSale names every listing of the seller's that counts toward the cap,
// newest first, and no other seller's.
func TestOnSaleListsTheCappedStatuses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, other := e.seller(t), e.seller(t)
	sold, hidden, active := e.active(t, seller), e.active(t, seller), e.active(t, seller)
	e.active(t, other)
	if _, err := e.catalog.MarkSold(ctx, seller, sold.ID, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.Hide(ctx, seller, hidden.ID, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	ids, err := e.catalog.OnSale(ctx, seller)
	if err != nil || !slices.Equal(ids, []uuid.UUID{active.ID, hidden.ID}) {
		t.Fatalf("%v %v, want %s %s", ids, err, active.ID, hidden.ID)
	}
}

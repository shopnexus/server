package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// attachThenFail is media whose first attach commits and then fails, as
// when the connection drops after the write.
type attachThenFail struct {
	mediasvc.Service
	restatetest.CommitThenFail
}

func (m *attachThenFail) Attach(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID) error {
	return m.After(m.Service.Attach(ctx, owner, holder, ids))
}

// A post whose attach committed and then failed goes on when Restate
// retries the step, and the listing goes on sale.
func TestPostSurvivesAFailedAttach(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.Media = &attachThenFail{Service: d.Media} })
	seller := e.seller(t)
	v := e.post(t, seller)
	if v = e.settle(t, seller, v.ID); v.Status != domain.StatusActive {
		t.Fatalf("%+v", v)
	}
}

// countsNothing reads every seller as having no listings, as a post that
// raced another past the count sees.
type countsNothing struct{ repository.Listings }

func (countsNothing) CountOnSale(context.Context, uuid.UUID) (int, error) { return 0, nil }

// A post that the insert finds at the cap takes no upload: the photo is
// still waiting, for the seller's next post.
func TestPostAtTheCapKeepsTheUploads(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.Listings = countsNothing{d.Listings} })
	ctx := context.Background()
	seller := e.seller(t)
	for range domain.MaxOnSale {
		l, err := domain.NewListing(uuid.NewV7(), seller, draft(uuid.NewV7()), domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err = e.listings.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	photo := e.upload(t, seller, jpegBytes(t))
	if _, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(photo)); !errors.Is(err, domain.ErrCapReached) {
		t.Fatalf("at the cap: %v", err)
	}
	if u, _ := mediarepo.NewUploads(e.db).Get(ctx, photo); u.Status() != mediadomain.StatusWaiting {
		t.Fatalf("the upload is %s", u.Status())
	}
}

// A post whose attach is refused leaves no listing behind.
func TestRefusedAttachUndoesTheListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	theirs := e.upload(t, e.seller(t), jpegBytes(t))
	if _, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(theirs)); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("%v", err)
	}
	p, err := e.catalog.ListMine(ctx, seller, service.PaneOnSale, page.Request{})
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}

// loseFirstEdit makes the first listing.updated save lose a race.
type loseFirstEdit struct {
	repository.Listings

	lost atomic.Bool
}

func (s *loseFirstEdit) Save(ctx context.Context, l *domain.Listing, writeID uuid.UUID, evs ...events.Event) error {
	if len(evs) == 1 && evs[0].Kind == events.ListingUpdated && !s.lost.Swap(true) {
		return repository.ErrConflict
	}
	return s.Listings.Save(ctx, l, writeID, evs...)
}

// An edit that added a photo and then lost a race is refused with
// try_again, and the seller's next try attaches the same photo again.
func TestEditAfterALostRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.Listings = &loseFirstEdit{Listings: d.Listings} })
	ctx := context.Background()
	seller := e.seller(t)
	v := e.active(t, seller)
	d := draft(v.Photos[0].UploadID, e.upload(t, seller, jpegBytes(t)))
	if _, err := e.catalog.Edit(ctx, seller, v.ID, uuid.NewV7().String(), d); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("first try: %v", err)
	}
	if _, err := e.catalog.Edit(ctx, seller, v.ID, uuid.NewV7().String(), d); err != nil {
		t.Fatalf("second try: %v", err)
	}
	if v = e.settle(t, seller, v.ID); v.Status != domain.StatusActive || len(v.Photos) != 2 || v.Photos[1].URL == "" {
		t.Fatalf("%+v", v)
	}
}

// Unmarking a sale counts toward the cap.
func TestUnmarkSoldHoldsTheCap(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	sold := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	for range domain.MaxOnSale {
		l, err := domain.NewListing(uuid.NewV7(), seller, draft(uuid.NewV7()), domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err = e.listings.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.catalog.UnmarkSold(ctx, seller, sold); !errors.Is(err, domain.ErrCapReached) {
		t.Fatalf("%v", err)
	}
}

// Two sellers who send the same Idempotency-Key each get their own post.
func TestIdempotencyKeysAreTheSellers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.NewV7().String()
	a, b := e.seller(t), e.seller(t)
	va, err := e.catalog.Post(ctx, a, key, draft(e.upload(t, a, jpegBytes(t))))
	if err != nil {
		t.Fatal(err)
	}
	vb, err := e.catalog.Post(ctx, b, key, draft(e.upload(t, b, jpegBytes(t))))
	if err != nil || vb.ID == va.ID || vb.SellerID != b {
		t.Fatalf("%+v %v", vb, err)
	}
}

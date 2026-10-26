package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Posting refuses a suspended account, then an account with no phone, then
// a seller at the cap, then a bad draft, then an upload that is not the
// seller's.
func TestPostChecksInOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	bad := draft()
	bad.CategoryCode = "phones" // a root, not a subcategory

	suspended := e.account(t)
	if _, err := e.accounts.Suspend(ctx, accountsvc.SuspendInput{ID: suspended, Reason: "spam", By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.Post(ctx, suspended, uuid.NewV7().String(), bad); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
	if _, err := e.catalog.Post(ctx, e.account(t), uuid.NewV7().String(), bad); !errors.Is(err, domain.ErrPhoneRequired) {
		t.Fatalf("no phone: %v", err)
	}

	full := e.seller(t)
	for range domain.MaxOnSale {
		l, err := domain.NewListing(uuid.NewV7(), full, draft(uuid.NewV7()), domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err = e.listings.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.catalog.Post(ctx, full, uuid.NewV7().String(), bad); !errors.Is(err, domain.ErrCapReached) {
		t.Fatalf("cap: %v", err)
	}

	seller := e.seller(t)
	bad.Photos = []uuid.UUID{e.upload(t, seller, jpegBytes(t))}
	bad.CommuneCode = "99999"
	_, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), bad)
	if ae, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidListing) ||
		ae.Fields["category_code"] == "" || ae.Fields["commune_code"] == "" {
		t.Fatalf("bad draft: %v", err)
	}
	theirs := e.upload(t, e.seller(t), jpegBytes(t))
	if _, err = e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(theirs)); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("someone else's upload: %v", err)
	}
}

// A posted listing turns active once its photos are processed, and a
// repeated post returns it.
func TestPostPublishes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	d := draft(e.upload(t, seller, jpegBytes(t)))
	mark := e.upload(t, seller, jpegBytes(t))
	d.Marks = []domain.MarkDraft{{Text: "Trầy góc", Photo: &mark}}
	key := uuid.NewV7().String()
	posted, err := e.catalog.Post(ctx, seller, key, d)
	if err != nil || posted.Status != domain.StatusProcessing || posted.Location.Province.DisplayName == "" {
		t.Fatalf("%+v %v", posted, err)
	}
	again, err := e.catalog.Post(ctx, seller, key, d)
	if err != nil || again.ID != posted.ID {
		t.Fatalf("repeat: %+v %v", again, err)
	}
	v := e.settle(t, seller, posted.ID)
	if v.Status != domain.StatusActive || v.PublishedAt == nil || v.Photos[0].URL == "" || v.Photos[0].ThumbURL == "" ||
		v.Marks[0].Photo == nil || v.Marks[0].Photo.URL == "" {
		t.Fatalf("%+v", v)
	}
	if got := kindsOf(t, e.db, posted.ID); !slices.Equal(got, []string{"listing.published"}) {
		t.Fatalf("events %v", got)
	}
	if _, err = e.catalog.Get(ctx, nil, posted.ID); err != nil {
		t.Fatalf("a buyer: %v", err)
	}
}

// A photo processing refuses rejects the listing with the reason; an edit
// that drops it sends the listing through processing again.
func TestPublishRejects(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	good, broken := e.upload(t, seller, jpegBytes(t)), e.upload(t, seller, []byte("not a jpeg at all"))
	posted, err := e.catalog.Post(ctx, seller, uuid.NewV7().String(), draft(good, broken))
	if err != nil {
		t.Fatal(err)
	}
	v := e.settle(t, seller, posted.ID)
	if v.Status != domain.StatusRejected || v.Photos[1].Failure == "" || v.Photos[0].Failure != "" {
		t.Fatalf("%+v", v)
	}
	if _, err = e.catalog.Get(ctx, nil, posted.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a buyer sees a rejected listing: %v", err)
	}
	if _, err = e.catalog.Edit(ctx, seller, posted.ID, uuid.NewV7().String(), draft(good)); err != nil {
		t.Fatal(err)
	}
	if v = e.settle(t, seller, posted.ID); v.Status != domain.StatusActive {
		t.Fatalf("%+v", v)
	}
}

// An edit that adds a photo takes the listing off sale until the photo is
// processed; one that changes only text keeps it on sale.
func TestEdit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	v := e.active(t, seller)
	d := draft(v.Photos[0].UploadID)
	d.Title = "iPhone 13 zin"
	edited, err := e.catalog.Edit(ctx, seller, v.ID, uuid.NewV7().String(), d)
	if err != nil || edited.Status != domain.StatusActive || edited.Title != "iPhone 13 zin" {
		t.Fatalf("%+v %v", edited, err)
	}
	d.Photos = append(d.Photos, e.upload(t, seller, jpegBytes(t)))
	if edited, err = e.catalog.Edit(ctx, seller, v.ID, uuid.NewV7().String(), d); err != nil || edited.Status != domain.StatusProcessing {
		t.Fatalf("%+v %v", edited, err)
	}
	if v = e.settle(t, seller, v.ID); v.Status != domain.StatusActive || v.Photos[1].URL == "" {
		t.Fatalf("%+v", v)
	}
	want := []string{"listing.published", "listing.updated", "listing.updated", "listing.published"}
	if got := kindsOf(t, e.db, v.ID); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	if _, err = e.catalog.Edit(ctx, e.seller(t), v.ID, uuid.NewV7().String(), d); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another seller: %v", err)
	}
}

// Each transition records its event; one the state does not allow is a
// conflict, and another seller's listing is not found.
func TestTransitions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	id := e.active(t, seller).ID
	other := e.seller(t)
	if _, err := e.catalog.Hide(ctx, other, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another seller: %v", err)
	}
	steps := []struct {
		name string
		do   func(context.Context, uuid.UUID, uuid.UUID) (service.ListingView, error)
		want domain.Status
	}{
		{"hide", e.catalog.Hide, domain.StatusHidden},
		{"unhide", e.catalog.Unhide, domain.StatusActive},
		{"mark sold", e.catalog.MarkSold, domain.StatusSold},
		{"unmark sold", e.catalog.UnmarkSold, domain.StatusActive},
	}
	for _, s := range steps {
		if v, err := s.do(ctx, seller, id); err != nil || v.Status != s.want {
			t.Fatalf("%s: %+v %v", s.name, v, err)
		}
	}
	if _, err := e.catalog.Unhide(ctx, seller, id); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("unhide active: %v", err)
	}
	if err := e.catalog.Delete(ctx, seller, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.Get(ctx, &seller, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	want := []string{
		"listing.published", "listing.hidden", "listing.unhidden", "listing.sold", "listing.unsold", "listing.deleted",
	}
	if got := kindsOf(t, e.db, id); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

// Only the seller sees a listing that is not active.
func TestGetHidesFromOthers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	id := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, id); err != nil {
		t.Fatal(err)
	}
	if v, err := e.catalog.Get(ctx, &seller, id); err != nil || v.Status != domain.StatusHidden {
		t.Fatalf("seller: %+v %v", v, err)
	}
	other := e.seller(t)
	for _, viewer := range []*uuid.UUID{nil, &other} {
		if _, err := e.catalog.Get(ctx, viewer, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("viewer %v: %v", viewer, err)
		}
	}
	if _, err := e.catalog.Get(ctx, nil, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

// The on-sale pane holds every listing not sold or deleted; the sold pane
// holds the sold ones.
func TestListMine(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	sold := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	gone := e.active(t, seller).ID
	if err := e.catalog.Delete(ctx, seller, gone); err != nil {
		t.Fatal(err)
	}
	onSale, err := e.catalog.ListMine(ctx, seller, service.PaneOnSale, page.Request{})
	if err != nil || len(onSale.Items) != 1 || onSale.Items[0].ID != hidden {
		t.Fatalf("on sale: %+v %v", onSale, err)
	}
	soldPane, err := e.catalog.ListMine(ctx, seller, service.PaneSold, page.Request{})
	if err != nil || len(soldPane.Items) != 1 || soldPane.Items[0].ID != sold {
		t.Fatalf("sold: %+v %v", soldPane, err)
	}
}

func TestCategoriesAndRegions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	cats, err := e.catalog.Categories(ctx)
	if err != nil || len(cats) != 55 {
		t.Fatalf("%d %v", len(cats), err)
	}
	provinces, err := e.catalog.Provinces(ctx)
	if err != nil || len(provinces) != 34 {
		t.Fatalf("%d %v", len(provinces), err)
	}
	communes, err := e.catalog.Communes(ctx, provinces[0].Code)
	if err != nil || len(communes) == 0 {
		t.Fatalf("%d %v", len(communes), err)
	}
}

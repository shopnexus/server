package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// known is a draft whose category and commune the tables hold.
var known = domain.Refs{CategoryIsLeaf: true, CommuneExists: true}

func draft() domain.Draft {
	return domain.Draft{
		Title: "  iPhone 13 128GB  ", Price: 9_500_000, Description: "Máy zin", CategoryCode: "phones.iphone",
		Condition: domain.ConditionLikeNew, AllowOffers: true, CommuneCode: "00004",
		Photos: []uuid.UUID{uuid.NewV7(), uuid.NewV7()},
		Marks:  []domain.MarkDraft{{Text: "Trầy góc", Photo: new(uuid.NewV7())}},
		Specs:  []domain.Spec{{Label: "Dung lượng", Value: "128GB"}},
	}
}

func newListing(t *testing.T) domain.Listing {
	t.Helper()
	l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), draft(), known, now)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewListingTrimsAndStartsProcessing(t *testing.T) {
	t.Parallel()
	l := newListing(t)
	if l.Title() != "iPhone 13 128GB" || l.Status() != domain.StatusProcessing || len(l.PendingUploads()) != 3 {
		t.Fatalf("%+v", l.State())
	}
}

func TestNewListingRules(t *testing.T) {
	t.Parallel()
	photos := func(n int) []uuid.UUID {
		out := make([]uuid.UUID, n)
		for i := range out {
			out[i] = uuid.NewV7()
		}
		return out
	}
	for field, change := range map[string]func(*domain.Draft){
		"title":       func(d *domain.Draft) { d.Title = "   " },
		"price":       func(d *domain.Draft) { d.Price = 9_999 },
		"description": func(d *domain.Draft) { d.Description = strings.Repeat("a", 1501) },
		"condition":   func(d *domain.Draft) { d.Condition = "broken" },
		"photos":      func(d *domain.Draft) { d.Photos = photos(11) },
		"marks":       func(d *domain.Draft) { d.Marks = []domain.MarkDraft{{Text: ""}} },
		"specs":       func(d *domain.Draft) { d.Specs = []domain.Spec{{Label: "x", Value: strings.Repeat("v", 51)}} },
	} {
		d := draft()
		change(&d)
		_, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, known, now)
		e, ok := errors.AsType[*apperr.Error](err)
		if !ok || !errors.Is(err, domain.ErrInvalidListing) || e.Fields[field] == "" {
			t.Errorf("%s: %v", field, err)
		}
	}
	d := draft()
	d.Photos = nil
	if _, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, known, now); !errors.Is(err, domain.ErrInvalidListing) {
		t.Error("no photo accepted")
	}
	for price, ok := range map[int64]bool{1_000_000_000: true, 1_000_000_001: false} {
		d = draft()
		d.Price = price
		_, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, known, now)
		if e, isErr := errors.AsType[*apperr.Error](err); (err == nil) != ok || (!ok && (!isErr || e.Fields["price"] == "")) {
			t.Errorf("price %d: %v", price, err)
		}
	}
	_, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), draft(), domain.Refs{}, now)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || e.Fields["category_code"] == "" || e.Fields["commune_code"] == "" {
		t.Errorf("unknown category and commune: %v", err)
	}
}

func published(t *testing.T) domain.Listing {
	t.Helper()
	l := newListing(t)
	keys := map[uuid.UUID]domain.PhotoKeys{}
	for _, id := range l.PendingUploads() {
		keys[id] = domain.PhotoKeys{Photo: "media/" + id.String() + ".jpg", Thumb: "media/" + id.String() + "_thumb.jpg"}
	}
	l.Published(keys, now)
	if l.Status() != domain.StatusActive || !l.PublishedAt().Equal(now) || len(l.PendingUploads()) != 0 {
		t.Fatalf("%+v", l.State())
	}
	return l
}

func TestTransitions(t *testing.T) {
	t.Parallel()
	conflict := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, domain.ErrStateConflict) {
			t.Errorf("%s: %v", name, err)
		}
	}
	processing := newListing(t)
	conflict("hide processing", processing.Hide(now))
	conflict("sell processing", processing.MarkSold(now))

	l := published(t)
	conflict("unhide active", l.Unhide(now))
	conflict("unsell active", l.UnmarkSold(now))
	if err := l.Hide(now); err != nil || l.Status() != domain.StatusHidden {
		t.Fatalf("hide: %v", err)
	}
	if err := l.Unhide(now); err != nil || l.Status() != domain.StatusActive {
		t.Fatalf("unhide: %v", err)
	}
	_ = l.Hide(now)
	if err := l.MarkSold(now); err != nil || l.Status() != domain.StatusSold || !l.SoldAt().Equal(now) {
		t.Fatalf("sell hidden: %v", err)
	}
	conflict("hide sold", l.Hide(now))
	if err := l.Edit(draft(), known, now); !errors.Is(err, domain.ErrStateConflict) {
		t.Errorf("edit sold: %v", err)
	}
	if err := l.UnmarkSold(now); err != nil || l.Status() != domain.StatusActive || l.SoldAt() != nil {
		t.Fatalf("unsell: %v", err)
	}
	if err := l.Delete(now); err != nil || l.Status() != domain.StatusDeleted {
		t.Fatalf("delete: %v", err)
	}
	conflict("delete twice", l.Delete(now))
	conflict("unhide deleted", l.Unhide(now))
}

func TestRejectedThenEdited(t *testing.T) {
	t.Parallel()
	l := newListing(t)
	bad := l.PendingUploads()[0]
	l.Rejected(map[uuid.UUID]string{bad: "not an image"}, now)
	if l.Status() != domain.StatusRejected || l.Rejection()[bad] != "not an image" {
		t.Fatalf("%+v", l.State())
	}
	d := draft()
	if err := l.Edit(d, known, now); err != nil || l.Status() != domain.StatusProcessing || len(l.Rejection()) != 0 {
		t.Fatalf("edit rejected: %v %s", err, l.Status())
	}
}

// An edit that keeps every photo keeps the listing's status; one that adds
// a photo sends it back through processing.
func TestEditOfAnActiveListing(t *testing.T) {
	t.Parallel()
	l := published(t)
	same := draft()
	same.Photos = photoUploads(l)
	same.Marks = nil
	same.Title = "iPhone 13"
	if err := l.Edit(same, known, now); err != nil || l.Status() != domain.StatusActive || l.Title() != "iPhone 13" {
		t.Fatalf("%v %s", err, l.Status())
	}
	more := same
	more.Photos = append(photoUploads(l), uuid.NewV7())
	if err := l.Edit(more, known, now); err != nil || l.Status() != domain.StatusProcessing || len(l.PendingUploads()) != 1 {
		t.Fatalf("%v %s", err, l.Status())
	}
}

func photoUploads(l domain.Listing) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(l.Photos()))
	for _, p := range l.Photos() {
		out = append(out, p.UploadID)
	}
	return out
}

func TestUploads(t *testing.T) {
	t.Parallel()
	d := draft()
	want := append(append([]uuid.UUID{}, d.Photos...), *d.Marks[0].Photo)
	if got := d.Uploads(); !slices.Equal(got, want) {
		t.Fatalf("draft: %v, want %v", got, want)
	}
	l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, known, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Uploads(); !slices.Equal(got, want) {
		t.Fatalf("listing: %v, want %v", got, want)
	}
}

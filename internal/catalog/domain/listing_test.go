package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/locale"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// known is a draft whose category and commune the tables hold.
var known = domain.Refs{CategoryIsLeaf: true, CommuneExists: true}

func draft() domain.ListingInput {
	return domain.ListingInput{
		Title: "  iPhone 13 128GB  ", Price: 9_500_000, Description: "Máy zin", CategoryCode: "phones.iphone",
		Condition: domain.ConditionLikeNew, AllowOffers: true, CommuneCode: "00004",
		Photos: []uuid.UUID{uuid.NewV7(), uuid.NewV7()},
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
	if l.Title() != "iPhone 13 128GB" || l.Status() != domain.StatusProcessing || len(l.PendingUploads()) != 2 {
		t.Fatalf("%+v", l.State())
	}
}

// UC-LST-01 E5 · BR-LST-03.
func TestNewListingRules(t *testing.T) {
	t.Parallel()
	photos := func(n int) []uuid.UUID {
		out := make([]uuid.UUID, n)
		for i := range out {
			out[i] = uuid.NewV7()
		}
		return out
	}
	for field, change := range map[string]func(*domain.ListingInput){
		"title":       func(d *domain.ListingInput) { d.Title = "   " },
		"price":       func(d *domain.ListingInput) { d.Price = 9_999 },
		"description": func(d *domain.ListingInput) { d.Description = strings.Repeat("a", 1501) },
		"condition":   func(d *domain.ListingInput) { d.Condition = "broken" },
		"photos":      func(d *domain.ListingInput) { d.Photos = photos(11) },
		"specs":       func(d *domain.ListingInput) { d.Specs = []domain.Spec{{Label: "x", Value: strings.Repeat("v", 51)}} },
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

// UC-LST-02 E1.
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

// A member's hold keeps a listing hidden: the seller cannot unhide it, and
// neither publishing an edit nor unmarking a sale puts it back on sale.
func TestHold(t *testing.T) {
	t.Parallel()
	conflict := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, domain.ErrStateConflict) {
			t.Errorf("%s: %v", name, err)
		}
	}
	processing := newListing(t)
	conflict("hold processing", processing.Hold(now))
	conflict("lift unheld", processing.Lift(now))

	l := published(t)
	if err := l.Hold(now); err != nil || l.Status() != domain.StatusHidden || l.HeldAt() == nil || !l.HeldAt().Equal(now) {
		t.Fatalf("hold active: %v %+v", err, l.State())
	}
	conflict("hold twice", l.Hold(now))
	if err := l.Unhide(now); !errors.Is(err, domain.ErrHeld) {
		t.Fatalf("seller unhides: %v", err)
	}
	conflict("seller hides", l.Hide(now))
	d := draft()
	d.Photos = append(d.Photos, uuid.NewV7())
	if err := l.Edit(d, known, now); err != nil || l.Status() != domain.StatusProcessing {
		t.Fatalf("edit held: %v %s", err, l.Status())
	}
	l.Published(nil, now)
	if l.Status() != domain.StatusHidden {
		t.Fatalf("published while held: %s", l.Status())
	}
	if err := l.MarkSold(now); err != nil {
		t.Fatal(err)
	}
	if err := l.UnmarkSold(now); err != nil || l.Status() != domain.StatusHidden {
		t.Fatalf("unsell held: %v %s", err, l.Status())
	}
	if err := l.Lift(now); err != nil || l.Status() != domain.StatusActive || l.HeldAt() != nil {
		t.Fatalf("lift: %v %+v", err, l.State())
	}
	conflict("lift twice", l.Lift(now))

	hidden := published(t)
	if err := hidden.Hide(now); err != nil {
		t.Fatal(err)
	}
	if err := hidden.Hold(now); err != nil || hidden.Status() != domain.StatusHidden || hidden.HeldAt() == nil {
		t.Fatalf("hold hidden: %v %+v", err, hidden.State())
	}
	if err := hidden.MarkSold(now); err != nil {
		t.Fatal(err)
	}
	if err := hidden.Lift(now); err != nil || hidden.Status() != domain.StatusSold || hidden.HeldAt() != nil {
		t.Fatalf("lift sold: %v %+v", err, hidden.State())
	}
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
	l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, known, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := l.Uploads(), d.Photos; !slices.Equal(got, want) {
		t.Fatalf("listing: %v, want %v", got, want)
	}
}

func TestReserveReleaseSell(t *testing.T) {
	t.Parallel()
	l := published(t)
	o, other := uuid.NewV7(), uuid.NewV7()
	if changed, err := l.Reserve(o, now); !changed || err != nil || l.Status() != domain.StatusReserved || l.ReservedFor() != o {
		t.Fatalf("reserve: %v %v %+v", changed, err, l.State())
	}
	if changed, err := l.Reserve(o, now); changed || err != nil {
		t.Fatalf("reserve again: %v %v", changed, err)
	}
	if _, err := l.Reserve(other, now); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("reserve for another: %v", err)
	}
	if l.Release(other, now) || l.Status() != domain.StatusReserved {
		t.Fatal("released for another order")
	}
	if !l.Release(o, now) || l.Status() != domain.StatusActive || l.ReservedFor() != (uuid.UUID{}) {
		t.Fatalf("release: %+v", l.State())
	}
	if _, err := l.Reserve(o, now); err != nil {
		t.Fatal(err)
	}
	if !l.SellTo(o, now) || l.Status() != domain.StatusSold || l.SoldAt() == nil || l.ReservedFor() != (uuid.UUID{}) {
		t.Fatalf("sell: %+v", l.State())
	}
	if l.SellTo(o, now) {
		t.Fatal("sold twice")
	}
}

func TestReservedRefuses(t *testing.T) {
	t.Parallel()
	l := published(t)
	if _, err := l.Reserve(uuid.NewV7(), now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func() error{
		"hide":      func() error { return l.Hide(now) },
		"mark sold": func() error { return l.MarkSold(now) },
		"delete":    func() error { return l.Delete(now) },
		"edit":      func() error { return l.Edit(draft(), known, now) },
	} {
		if err := change(); !errors.Is(err, domain.ErrStateConflict) {
			t.Errorf("%s: %v", name, err)
		}
	}
	hidden := published(t)
	if err := hidden.Hide(now); err != nil {
		t.Fatal(err)
	}
	for name, x := range map[string]domain.Listing{"hidden": hidden, "processing": newListing(t)} {
		if _, err := x.Reserve(uuid.NewV7(), now); !errors.Is(err, domain.ErrStateConflict) {
			t.Errorf("reserve %s: %v", name, err)
		}
	}
}

// A listing measures distance from the seller's point when they gave one,
// keeps it through edits without one, and takes the commune's point
// otherwise or when the commune changes.
func TestListingPoint(t *testing.T) {
	t.Parallel()
	commune := domain.Point{Lat: 21.03, Lng: 105.85}
	refs := domain.Refs{CategoryIsLeaf: true, CommuneExists: true, CommunePoint: commune}
	d := draft()
	l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, refs, now)
	if err != nil || l.Point() != commune {
		t.Fatalf("no gps: %v %v", l.Point(), err)
	}
	spot := domain.Point{Lat: 21.04, Lng: 105.86}
	d.Spot = &spot
	if err = l.Edit(d, refs, now); err != nil || l.Point() != spot {
		t.Fatalf("gps: %v %v", l.Point(), err)
	}
	d.Spot = nil
	if err = l.Edit(d, refs, now); err != nil || l.Point() != spot {
		t.Fatalf("an edit without gps kept %v, %v", l.Point(), err)
	}
	other := domain.Point{Lat: 10.77, Lng: 106.70}
	d.CommuneCode = "26734"
	if err = l.Edit(d, domain.Refs{CategoryIsLeaf: true, CommuneExists: true, CommunePoint: other}, now); err != nil || l.Point() != other {
		t.Fatalf("a new commune: %v %v", l.Point(), err)
	}
	far := domain.Point{Lat: 21.5, Lng: 105.85}
	d = draft()
	d.Spot = &far
	_, err = domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, refs, now)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || e.Fields["gps"] == "" {
		t.Fatalf("a point 52 km away: %v", err)
	}
}

// Only the first publication is first: not one after a rejection, nor one
// after an edit that added a photo.
func TestPublishedTellsTheFirst(t *testing.T) {
	t.Parallel()
	l := newListing(t)
	keys := func() map[uuid.UUID]domain.PhotoKeys {
		k := map[uuid.UUID]domain.PhotoKeys{}
		for _, id := range l.PendingUploads() {
			k[id] = domain.PhotoKeys{Photo: "media/" + id.String() + ".jpg", Thumb: "media/" + id.String() + "_thumb.jpg"}
		}
		return k
	}
	if !l.Published(keys(), now) {
		t.Fatal("the first publication is not first")
	}
	d := draft()
	d.Photos = append(l.Uploads(), uuid.NewV7())
	if err := l.Edit(d, known, now.Add(time.Hour)); err != nil || l.Status() != domain.StatusProcessing {
		t.Fatalf("edit: %v %s", err, l.Status())
	}
	l.Rejected(map[uuid.UUID]string{d.Photos[len(d.Photos)-1]: "nsfw"}, now.Add(time.Hour))
	if err := l.Edit(d, known, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l.Published(keys(), now.Add(2*time.Hour)) {
		t.Fatal("a publication after a rejection is first")
	}
	if !l.PublishedAt().Equal(now) {
		t.Fatalf("published_at moved: %v", l.PublishedAt())
	}
}

// storage and color are two specs a category asks for.
var storage, color = domain.SpecNames{Key: "storage", Names: map[locale.Tag]string{locale.Vietnamese: "Dung lượng", locale.English: "Storage"}},
	domain.SpecNames{Key: "color", Names: map[locale.Tag]string{locale.Vietnamese: "Màu", locale.English: "Color"}}

func TestCategorySpecsRequired(t *testing.T) {
	t.Parallel()
	refs := domain.Refs{CategoryIsLeaf: true, CommuneExists: true, Specs: []domain.SpecNames{storage, color}}
	_, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), draft(), refs, now)
	var e *apperr.Error
	if !errors.As(err, &e) || !strings.Contains(e.Fields["specs"], "Màu") || strings.Contains(e.Fields["specs"], "Dung lượng") {
		t.Fatalf("missing Màu: %v", err)
	}
	if e.FieldCodes["specs"] != apperr.MissingSpecs || !slices.Equal(e.Details.MissingSpecs, []string{"Màu"}) ||
		!slices.Equal(e.Details.MissingSpecKeys, []string{"color"}) {
		t.Fatalf("code %q, details %+v", e.FieldCodes["specs"], e.Details)
	}
	// A spec by key, by its Vietnamese or English name (case aside), or
	// one the seller names, stores the same keys and the Vietnamese names.
	want := []domain.Spec{{Key: "storage", Label: "Dung lượng", Value: "128GB"}, {Key: "color", Label: "Màu", Value: "Trắng"}, {Label: "Hộp", Value: "Còn"}}
	for name, specs := range map[string][]domain.Spec{
		"keys":       {{Key: "storage", Value: "128GB"}, {Key: "color", Label: "Colour", Value: "Trắng"}, {Label: "Hộp", Value: "Còn"}},
		"Vietnamese": {{Label: "Dung lượng", Value: "128GB"}, {Label: "màu", Value: "Trắng"}, {Label: " Hộp ", Value: "Còn"}},
		"English":    {{Label: "storage", Value: "128GB"}, {Label: "Color", Value: " Trắng"}, {Label: "Hộp", Value: "Còn"}},
	} {
		d := draft()
		d.Specs = specs
		l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, refs, now)
		if err != nil || !slices.Equal(l.Specs(), want) {
			t.Errorf("%s: %+v %v", name, l.Specs(), err)
		}
	}
	for name, c := range map[string]struct {
		specs []domain.Spec
		code  apperr.FieldCode
	}{
		"an unknown key":   {[]domain.Spec{{Key: "battery", Value: "90%"}}, apperr.Unknown},
		"a key twice":      {[]domain.Spec{{Label: "Màu", Value: "Đen"}}, apperr.Duplicate},
		"no key, no label": {[]domain.Spec{{Value: "Trắng"}}, apperr.Required},
	} {
		d := draft()
		d.Specs = append(d.Specs, domain.Spec{Key: "color", Value: "Trắng"})
		d.Specs = append(d.Specs, c.specs...)
		_, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, refs, now)
		if !errors.As(err, &e) || e.FieldCodes["specs"] != c.code {
			t.Errorf("%s: %v %v", name, e.FieldCodes, err)
		}
	}
}

package domain_test

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func content(title string, photos ...uuid.UUID) domain.DraftContent {
	return domain.DraftContent{SchemaVersion: 1, Title: title, Price: new(int64(1_500_000)), Photos: photos, Body: `{"condition":"used"}`}
}

// A draft lives 30 days from its last write; a replace keeps its creation
// and keeps each photo once.
func TestNewAndReplace(t *testing.T) {
	t.Parallel()
	a, b := uuid.NewV7(), uuid.NewV7()
	d := domain.NewListingDraft(uuid.NewV7(), uuid.NewV7(), content("Xe đạp", a, b, a), now)
	if !slices.Equal(d.Photos(), []uuid.UUID{a, b}) || !d.ExpiresAt().Equal(now.Add(domain.DraftLife)) || !d.CreatedAt().Equal(now) {
		t.Fatalf("new: %+v", d.State())
	}
	later := now.Add(20 * 24 * time.Hour)
	d.Replace(content("Xe đạp Asama", b), later)
	if d.Content().Title != "Xe đạp Asama" || !d.UpdatedAt().Equal(later) || !d.ExpiresAt().Equal(later.Add(domain.DraftLife)) ||
		!d.CreatedAt().Equal(now) || !slices.Equal(d.Photos(), []uuid.UUID{b}) {
		t.Fatalf("replaced: %+v", d.State())
	}
}

// Another account's ID answers as no draft would; a create needs a free
// place, a replace does not.
func TestCheckPut(t *testing.T) {
	t.Parallel()
	me := uuid.NewV7()
	mine := domain.NewListingDraft(uuid.NewV7(), me, content(""), now)
	theirs := domain.NewListingDraft(uuid.NewV7(), uuid.NewV7(), content(""), now)
	for name, c := range map[string]struct {
		stored *domain.ListingDraft
		count  int
		want   error
	}{
		"create":                     {nil, domain.MaxDrafts - 1, nil},
		"create at the limit":        {nil, domain.MaxDrafts, domain.ErrDraftLimitReached},
		"replace at the limit":       {&mine, domain.MaxDrafts, nil},
		"another account's draft ID": {&theirs, 0, domain.ErrDraftNotFound},
	} {
		err := domain.CheckPut(c.stored, me, c.count)
		if (c.want == nil) != (err == nil) || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

// A photo a listing took or that expired leaves the draft; the others keep
// their order, each once.
func TestKeepPhotos(t *testing.T) {
	t.Parallel()
	a, b, c, d := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if got := domain.KeepPhotos([]uuid.UUID{c, a, b, a, d}, []uuid.UUID{b, d}); !slices.Equal(got, []uuid.UUID{c, a}) {
		t.Fatalf("%v", got)
	}
	if got := domain.KeepPhotos(nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("none: %#v", got)
	}
}

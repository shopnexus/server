package domain_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

func TestSplitText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want domain.Text
	}{
		{"  iPhone   11\tTrắng \n\n Máy dùng 3 năm.\nCòn hộp. ", domain.Text{Title: "iPhone 11 Trắng", Description: "Máy dùng 3 năm.\nCòn hộp."}},
		{"iPhone 1", domain.Text{Title: "iPhone 1"}},
		{"iPhone 11\n", domain.Text{Title: "iPhone 11"}},
		{"NO_", domain.Text{}},
		{"NO_ITEM", domain.Text{}},
		{"NO_ITEM\n", domain.Text{}},
		{"", domain.Text{}},
		{"Tiêu đề: Quạt bàn Senko\n\nMô tả: Quạt màu trắng.", domain.Text{Title: "Quạt bàn Senko", Description: "Quạt màu trắng."}},
		{"Tiêu đề:\nQuạt bàn Senko\n\nMô tả:\nQuạt màu trắng.", domain.Text{Title: "Quạt bàn Senko", Description: "Quạt màu trắng."}},
	}
	for _, c := range cases {
		if got := domain.SplitText(c.raw, 300); got != c.want {
			t.Errorf("%q: %+v", c.raw, got)
		}
	}
	long := domain.SplitText(strings.Repeat("ắ", 100)+"\n"+strings.Repeat("ư", 400), 300)
	if utf8.RuneCountInString(long.Title) != catalogdomain.MaxTitle || utf8.RuneCountInString(long.Description) != 300 {
		t.Fatalf("cut: %d %d", utf8.RuneCountInString(long.Title), utf8.RuneCountInString(long.Description))
	}
}

func eightFrames() []domain.Photo {
	out := make([]domain.Photo, 8)
	for i := range out {
		out[i] = domain.Photo{UploadID: uuid.NewV7(), AtSeconds: new(float64(i))}
	}
	return out
}

func TestVideoDraft(t *testing.T) {
	t.Parallel()
	frames := eightFrames()
	d, err := domain.VideoDraft("Xe đạp Asama xanh\nXe đi ít.", frames, 3)
	if err != nil || d.Title != "Xe đạp Asama xanh" || d.Description != "Xe đi ít." || d.Condition != catalogdomain.ConditionUsed ||
		d.Price != nil || d.AllowOffers || d.CategoryCode != "" || d.Specs == nil || len(d.Specs) != 0 {
		t.Fatalf("%+v %v", d, err)
	}
	want := []uuid.UUID{frames[3].UploadID, frames[0].UploadID, frames[1].UploadID, frames[2].UploadID, frames[4].UploadID, frames[5].UploadID}
	if len(d.Photos) != domain.MaxKept || !equalIDs(d.Photos, want) {
		t.Fatalf("photos %v, want the sharpest then time order", d.Photos)
	}
	if d, err = domain.VideoDraft("Xe đạp", nil, 0); err != nil || len(d.Photos) != 0 {
		t.Fatalf("no frames: %+v %v", d, err)
	}
	for _, raw := range []string{"NO_ITEM", "  "} {
		if _, err = domain.VideoDraft(raw, frames, 0); !errors.Is(err, domain.ErrNoItem) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestPhotoDraft(t *testing.T) {
	t.Parallel()
	photo := uuid.NewV7()
	d, err := domain.PhotoDraft("iPhone 11 Trắng\n"+strings.Repeat("a", 400), photo)
	if err != nil || d.Title != "iPhone 11 Trắng" || utf8.RuneCountInString(d.Description) != domain.MaxPhotoDescription ||
		len(d.Photos) != 1 || d.Photos[0] != photo || d.Condition != catalogdomain.ConditionUsed {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err = domain.PhotoDraft("NO_ITEM", photo); !errors.Is(err, domain.ErrNoItem) {
		t.Fatalf("no item: %v", err)
	}
}

func TestLinkDraft(t *testing.T) {
	t.Parallel()
	d, removed, err := domain.LinkDraft("iPhone 11 64GB Trắng\nMáy zin. Liên hệ 0912 345 678.", 4_500_000)
	if err != nil || !removed || d.Description != "Máy zin. Liên hệ." || d.Price == nil || *d.Price != 4_500_000 || len(d.Photos) != 0 {
		t.Fatalf("%+v %v %v", d, removed, err)
	}
	if d, removed, err = domain.LinkDraft("iPhone 11\nMáy zin.", 5_000); err != nil || removed || d.Price != nil {
		t.Fatalf("a price under 10,000: %+v %v %v", d, removed, err)
	}
	if _, _, err = domain.LinkDraft("NO_ITEM", 4_500_000); !errors.Is(err, domain.ErrNoItem) {
		t.Fatalf("no item: %v", err)
	}
}

func TestLinkTextHidesAPhoneBeingWritten(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"iPhone 11\nLiên hệ 0912 34":          "Liên hệ",
		"iPhone 11\nLiên hệ +84 91":           "Liên hệ",
		"iPhone 11\nLiên hệ 0912 345 678 nhé": "Liên hệ nhé",
		"iPhone 11\nMáy zin":                  "Máy zin",
	} {
		if got := domain.LinkText(raw); got.Description != want || got.Title != "iPhone 11" {
			t.Errorf("%q: %+v", raw, got)
		}
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

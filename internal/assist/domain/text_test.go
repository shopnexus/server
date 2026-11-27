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

func item(title, description string) domain.Reply {
	return domain.Reply{Subject: "x", Found: true, Title: title, Description: description}
}

func TestParseReply(t *testing.T) {
	t.Parallel()
	want := domain.Reply{Subject: "xe đạp", Found: true, Title: "Xe đạp", Description: "Xe trắng."}
	for _, raw := range []string{
		`{"subject":"xe đạp","found":true,"title":"Xe đạp","description":"Xe trắng.","reason":""}`,
		"```json\n{\"subject\": \"xe đạp\", \"found\": true, \"title\": \"Xe đạp\", \"description\": \"Xe trắng.\"}\n```",
	} {
		if got, err := domain.ParseReply(raw); err != nil || got != want {
			t.Errorf("%q: %+v %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "Xe đạp\nXe trắng.", `{"found": tru`} {
		if _, err := domain.ParseReply(raw); !errors.Is(err, domain.ErrBadReply) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

// The text shows as the model writes it, once it says it found an item.
func TestPartialText(t *testing.T) {
	t.Parallel()
	whole := `{"subject":"quạt","found":true,"title":"Quạt  bàn\tSenko","description":"Quạt \"trắng\"\nCòn hộp. \u0110ẹp","reason":""}`
	var last domain.Text
	for i := range len(whole) + 1 {
		last = domain.PartialText(whole[:i], 300)
		if strings.ContainsAny(last.Description, "\\") {
			t.Fatalf("cut at %d: an escape leaked: %+v", i, last)
		}
	}
	if last != (domain.Text{Title: "Quạt bàn Senko", Description: "Quạt \"trắng\"\nCòn hộp. Đẹp"}) {
		t.Fatalf("%+v", last)
	}
	for _, raw := range []string{
		`{"subject":"quạt","found":true,"title":"Quạt bà`,
		`{"subject": "quạt" , "found" : true, "title": "Quạt bà`,
	} {
		if got := domain.PartialText(raw, 300); got.Title != "Quạt bà" {
			t.Errorf("%q: %+v", raw, got)
		}
	}
	for _, raw := range []string{"", `{"subject":"một người","found":false,"title":"x"`, `{"subject":"quạt","title":"Quạt"`, "Quạt bàn"} {
		if got := domain.PartialText(raw, 300); got != (domain.Text{}) {
			t.Errorf("%q: %+v", raw, got)
		}
	}
	long := domain.PartialText(`{"found":true,"title":"`+strings.Repeat("ắ", 100)+`","description":"`+strings.Repeat("ư", 400)+`"}`, 300)
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
	d, err := domain.VideoDraft(item("Xe đạp Asama xanh", "Xe đi ít."), frames, 3)
	if err != nil || d.Title != "Xe đạp Asama xanh" || d.Description != "Xe đi ít." || d.Condition != catalogdomain.ConditionUsed ||
		d.Price != nil || d.AllowOffers || d.CategoryCode != "" || d.Specs == nil || len(d.Specs) != 0 {
		t.Fatalf("%+v %v", d, err)
	}
	want := []uuid.UUID{frames[3].UploadID, frames[0].UploadID, frames[1].UploadID, frames[2].UploadID, frames[4].UploadID, frames[5].UploadID}
	if len(d.Photos) != domain.MaxKept || !equalIDs(d.Photos, want) {
		t.Fatalf("photos %v, want the sharpest then time order", d.Photos)
	}
	if d, err = domain.VideoDraft(item("Xe đạp", ""), nil, 0); err != nil || len(d.Photos) != 0 {
		t.Fatalf("no frames: %+v %v", d, err)
	}
	for _, r := range []domain.Reply{{Found: false, Reason: "Chỉ có người."}, item("  ", "x")} {
		if _, err = domain.VideoDraft(r, frames, 0); !errors.Is(err, domain.ErrNoItem) {
			t.Errorf("%+v: %v", r, err)
		}
	}
}

func TestPhotoDraft(t *testing.T) {
	t.Parallel()
	photo := uuid.NewV7()
	d, err := domain.PhotoDraft(item("iPhone 11 Trắng", strings.Repeat("a", 400)), photo)
	if err != nil || d.Title != "iPhone 11 Trắng" || utf8.RuneCountInString(d.Description) != domain.MaxPhotoDescription ||
		len(d.Photos) != 1 || d.Photos[0] != photo || d.Condition != catalogdomain.ConditionUsed {
		t.Fatalf("%+v %v", d, err)
	}
	_, err = domain.PhotoDraft(domain.Reply{Subject: "một người", Reason: "Ảnh chỉ có một người."}, photo)
	if noItem, ok := errors.AsType[domain.NoItemError](err); !ok || !errors.Is(err, domain.ErrNoItem) || noItem.Reason != "Ảnh chỉ có một người." {
		t.Fatalf("no item: %v", err)
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

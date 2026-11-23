package domain_test

import (
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

func TestNewFeedback(t *testing.T) {
	t.Parallel()
	d, err := domain.NewFeedback(domain.Draft{Kind: "idea", Text: "  Thêm bộ lọc size khung  ", Screen: " search_results ", Contact: true})
	if err != nil || d.Text != "Thêm bộ lọc size khung" || d.Screen != "search_results" || d.Kind != domain.KindIdea || d.Source != domain.SourceApp {
		t.Fatalf("%+v %v", d, err)
	}
	if d, err = domain.NewFeedback(domain.Draft{Mood: 4, Text: "   "}); err != nil || d.Text != "" || d.Mood != 4 {
		t.Fatalf("a mood alone: %+v %v", d, err)
	}
	d, err = domain.NewFeedback(domain.Draft{Mood: 5, Source: domain.SourceWeb, PageURL: " https://shopnexus.id.vn/search?q=laptop "})
	if err != nil || d.Source != domain.SourceWeb || d.PageURL != "https://shopnexus.id.vn/search?q=laptop" {
		t.Fatalf("from the website: %+v %v", d, err)
	}
	for name, c := range map[string]struct {
		mood   int
		kind   string
		text   string
		photos int
		source string
		page   string
		field  string
		code   apperr.FieldCode
	}{
		"neither":      {text: "  ", field: "text", code: apperr.Required},
		"mood of 6":    {mood: 6, text: "x", field: "mood", code: apperr.OutOfRange},
		"other kind":   {mood: 1, kind: "rant", field: "kind", code: apperr.NotAllowed},
		"long text":    {text: strings.Repeat("a", domain.MaxText+1), field: "text", code: apperr.TooLong},
		"4 photos":     {mood: 2, photos: 4, field: "photos", code: apperr.TooMany},
		"other source": {mood: 3, source: "tv", field: "source", code: apperr.NotAllowed},
		"long page":    {mood: 3, source: "web", page: strings.Repeat("a", 501), field: "page_url", code: apperr.TooLong},
	} {
		_, err = domain.NewFeedback(domain.Draft{
			Mood: c.mood, Kind: domain.Kind(c.kind), Text: c.text, Photos: make([]uuid.UUID, c.photos),
			Source: domain.Source(c.source), PageURL: c.page,
		})
		var e *apperr.Error
		if !errors.As(err, &e) || !errors.Is(err, domain.ErrInvalid) || e.FieldCodes[c.field] != c.code {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNewStatusChange(t *testing.T) {
	t.Parallel()
	c, err := domain.NewStatusChange("shipped", " Bộ lọc Size khung đã có ", " 2.5 ")
	if err != nil || c.Note != "Bộ lọc Size khung đã có" || c.Version != "2.5" {
		t.Fatalf("%+v %v", c, err)
	}
	if c, err = domain.NewStatusChange("in_progress", "  ", ""); err != nil || c.Note != "" {
		t.Fatalf("a blank note is none: %+v %v", c, err)
	}
	for name, in := range map[string][3]string{
		"version without shipped": {"in_progress", "Dự kiến bản 2.6", "2.6"},
		"unknown status":          {"done", "", ""},
		"long note":               {"seen", strings.Repeat("a", 81), ""},
	} {
		if _, err = domain.NewStatusChange(in[0], in[1], in[2]); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !domain.StatusShipped.Tells() || !domain.StatusInProgress.Tells() || domain.StatusSeen.Tells() || domain.StatusSent.Tells() {
		t.Error("only in_progress and shipped tell the owner")
	}
}

func TestReplyText(t *testing.T) {
	t.Parallel()
	if got, err := domain.ReplyText("  Cảm ơn bạn  "); err != nil || got != "Cảm ơn bạn" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := domain.ReplyText("   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank: %v", err)
	}
}

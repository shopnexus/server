package service

import (
	"strings"
	"testing"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

func TestSystemPromptListsSubcategories(t *testing.T) {
	t.Parallel()
	p := systemPrompt([]catalogdomain.Category{
		{Code: "phones", DisplayName: "Điện thoại"},
		{Code: "phones.iphone", ParentCode: "phones", DisplayName: "iPhone"},
		{Code: "phones.other", ParentCode: "phones", DisplayName: "Khác"},
		{Code: "fashion", DisplayName: "Thời trang"},
		{Code: "fashion.shoes", ParentCode: "fashion", DisplayName: "Giày dép"},
	})
	for _, line := range []string{
		"\nphones.iphone — Điện thoại › iPhone\n", "\nphones.other — Điện thoại › Khác\n", "\nfashion.shoes — Thời trang › Giày dép\n",
	} {
		if !strings.Contains(p, line) {
			t.Errorf("missing %q", line)
		}
	}
	if strings.Contains(p, "\nphones — ") || strings.Contains(p, "\nfashion — ") {
		t.Error("a root is listed as a category")
	}
	// The rule of app/mohinh.md section 1: the model names no mark.
	if !strings.Contains(p, "Never name a mark, scratch, dent, crack, stain or fault the seller did not name") {
		t.Error("the marks rule is missing")
	}
}

func TestUserPrompt(t *testing.T) {
	t.Parallel()
	p := userPrompt("mình bán bốn triệu rưỡi có thương lượng", 8)
	if !strings.Contains(p, "8 frames, numbered 0 to 7") ||
		!strings.Contains(p, "<transcript>\nmình bán bốn triệu rưỡi có thương lượng\n</transcript>") {
		t.Fatalf("%s", p)
	}
	if p = userPrompt("  ", 1); !strings.Contains(p, "1 frame, numbered 0") || !strings.Contains(p, "<transcript>\n</transcript>") ||
		!strings.Contains(p, "The seller said nothing") {
		t.Fatalf("%s", p)
	}
}

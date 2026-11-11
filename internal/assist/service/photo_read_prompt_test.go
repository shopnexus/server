package service

import (
	"strings"
	"testing"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

func TestPhotoSystemPrompt(t *testing.T) {
	t.Parallel()
	p := photoSystemPrompt([]catalogdomain.Category{
		{Code: "phones", DisplayName: "Điện thoại"},
		{Code: "phones.iphone", ParentCode: "phones", DisplayName: "iPhone"},
	})
	for _, want := range []string{
		"\nphones.iphone — Điện thoại › iPhone\n",
		"at most 300 characters",
		"Never name a mark, scratch, dent, crack, stain or fault",
		"no age, use, repairs, battery health",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(p, "\nphones — ") {
		t.Error("a root is listed as a category")
	}
}

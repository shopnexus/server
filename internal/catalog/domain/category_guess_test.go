package domain_test

import (
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func TestGuessTextsNameEachKeyword(t *testing.T) {
	t.Parallel()
	tree := []domain.Category{
		{Code: "phones", DisplayName: "Điện thoại"},
		{Code: "phones.iphone", ParentCode: "phones", DisplayName: "iPhone"},
		{Code: "phones.other", ParentCode: "phones", DisplayName: "Khác"},
	}
	texts, codes := domain.GuessTexts(tree)
	if len(texts) != 1+len(domain.CategoryKeywords["phones.iphone"]) || len(codes) != len(texts) {
		t.Fatalf("texts = %q", texts)
	}
	if texts[0] != "Điện thoại › iPhone" || !strings.HasPrefix(texts[1], "Điện thoại › iPhone: ") {
		t.Fatalf("texts = %q", texts[:2])
	}
	for _, c := range codes {
		if c != "phones.iphone" {
			t.Fatalf("code %q: a root and Khác have no texts", c)
		}
	}
}

func TestGuessCategoryTakesTheNearest(t *testing.T) {
	t.Parallel()
	protos := []domain.CategoryPrototype{
		{Code: "a.x", Vector: []float32{1, 0}},
		{Code: "b.y", Vector: []float32{0, 1}},
		{Code: "a.x", Vector: []float32{0.6, 0.8}},
	}
	if got := domain.GuessCategory([]float32{0.5, 0.86}, protos); got != "a.x" {
		t.Fatalf("guess = %q, want a.x by its second text", got)
	}
	if got := domain.GuessCategory([]float32{1, 0}, nil); got != "" {
		t.Fatalf("guess without prototypes = %q", got)
	}
}

func TestGuessText(t *testing.T) {
	t.Parallel()
	if got := domain.GuessText("  Xe đạp ", ""); got != "Xe đạp" {
		t.Fatalf("title only = %q", got)
	}
	if got := domain.GuessText(" ", "\n"); got != "" {
		t.Fatalf("blank = %q", got)
	}
	long := domain.GuessText("đ", strings.Repeat("ạ", domain.EmbeddingTextMax))
	if len(long) > domain.EmbeddingTextMax || !strings.HasPrefix(long, "đ\nạ") {
		t.Fatalf("long text: %d bytes", len(long))
	}
}

func TestKeywordsAreOnLeavesOtherThanKhac(t *testing.T) {
	t.Parallel()
	for code, words := range domain.CategoryKeywords {
		root, leaf, ok := strings.Cut(code, ".")
		if !ok || root == "" || leaf == "" || leaf == "other" {
			t.Errorf("%s: keywords go on a subcategory other than Khác", code)
		}
		if len(words) == 0 {
			t.Errorf("%s: no keyword", code)
		}
	}
}

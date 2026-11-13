package domain_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// TestHoldsAllWords holds phrases against listing text as the sweep's
// all-words query decides them; words_index_test.go in the service runs
// the same cases against the index.
func TestHoldsAllWords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, phrase, text string
		want               bool
	}{
		{"every word", "xe đạp", "Xe đạp Asama đi ít", true},
		{"case", "IPHONE 11", "iphone 11 64GB", true},
		{"order does not matter", "đạp xe", "xe đạp", true},
		{"a missing word", "xe đạp", "xe máy Honda", false},
		{"diacritics are letters", "xe dap", "Xe đạp", false},
		{"punctuation splits", "64 gb", "iPhone 11 (64-GB)", true},
		{"digits are words", "11", "iphone 11", true},
		{"part of a word is not a word", "phone", "iphone 11", false},
		{"an empty phrase matches nothing", " - ", "xe đạp", false},
	} {
		if got := domain.HoldsAllWords(c.phrase, c.text); got != c.want {
			t.Errorf("%s: HoldsAllWords(%q, %q) = %v", c.name, c.phrase, c.text, got)
		}
	}
}

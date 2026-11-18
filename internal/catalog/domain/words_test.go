package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
)

// TestHoldsAllWords splits the phrase as the sweep does and looks its words
// up among the text's words as the index split them (catalog's Words);
// words_index_test.go in the service runs the pair against the index.
func TestHoldsAllWords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, phrase string
		words        []string
		want         bool
	}{
		{"every word", "xe đạp", []string{"xe", "đạp", "asama"}, true},
		{"case", "IPHONE 11", []string{"iphone", "11", "64gb"}, true},
		{"order does not matter", "đạp xe", []string{"xe", "đạp"}, true},
		{"a missing word", "xe đạp", []string{"xe", "máy"}, false},
		{"punctuation splits the phrase", "64-gb", []string{"64", "gb"}, true},
		{"a decimal the index keeps whole", "dell 15", []string{"laptop", "dell", "15.6"}, false},
		{"an empty phrase matches nothing", " - ", []string{"xe"}, false},
	} {
		if got := domain.HoldsAllWords(c.phrase, c.words); got != c.want {
			t.Errorf("%s: HoldsAllWords(%q, %q) = %v", c.name, c.phrase, c.words, got)
		}
	}
}

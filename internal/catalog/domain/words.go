package domain

import (
	"strings"
	"unicode"
)

// HoldsAllWords reports whether every word of phrase is among words, a
// text's words as the search index splits them (catalog's Words). The
// phrase is split as the sweep's all-words query splits it, on every rune
// that is not a letter or a digit, and lowercased. A phrase without words
// holds nothing.
func HoldsAllWords(phrase string, words []string) bool {
	want := strings.FieldsFunc(strings.ToLower(phrase), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(want) == 0 {
		return false
	}
	have := make(map[string]bool, len(words))
	for _, w := range words {
		have[w] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

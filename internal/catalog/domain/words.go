package domain

import (
	"strings"
	"unicode"
)

// HoldsAllWords reports whether every word of phrase is a word of text, as
// the search index matches a watch's phrase: both split on every rune that
// is not a letter or a digit and lowercased, with no folding of
// diacritics. A phrase without words holds nothing.
func HoldsAllWords(phrase, text string) bool {
	want := words(phrase)
	if len(want) == 0 {
		return false
	}
	have := make(map[string]bool)
	for _, w := range words(text) {
		have[w] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

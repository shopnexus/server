// Package textkey folds text people type into a key that matches the same
// words typed another way.
package textkey

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold lowercases s, collapses its spaces and drops Vietnamese marks, đ as
// d, so "Xe  Đạp" and "xe dap" are one key.
func Fold(s string) string {
	key := strings.ToLower(strings.Join(strings.Fields(s), " "))
	// A transformer keeps state, so each call builds its own.
	folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), key)
	if err != nil {
		folded = key
	}
	return strings.ReplaceAll(folded, "đ", "d")
}

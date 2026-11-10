package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// MaxHistory is how many searches a person's history keeps.
const MaxHistory = 10

// HistoryKey is a query as the history keys it: QueryKey, then without
// Vietnamese marks, đ as d, so "Xe Đạp" and "xe dap" are one entry.
func HistoryKey(q string) string {
	key := QueryKey(q)
	// A transformer keeps state, so each call builds its own.
	folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), key)
	if err != nil {
		folded = key
	}
	return strings.ReplaceAll(folded, "đ", "d")
}

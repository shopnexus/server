package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
)

// HoldsAllWords stands in for the index when a listing is not posted yet,
// so each case puts a listing titled with the case's text and runs the
// sweep's all-words search on it. A disagreement means the tokenizer
// changed, and HoldsAllWords follows the index.
func TestHoldsAllWordsAgreesWithTheIndex(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.account(t)
	for _, c := range []struct {
		name, phrase, text string
	}{
		{"every word", "xe đạp", "Xe đạp Asama đi ít"},
		{"case", "IPHONE 11", "iphone 11 64GB"},
		{"order does not matter", "đạp xe", "xe đạp"},
		{"a missing word", "xe đạp", "xe máy Honda"},
		{"diacritics are letters", "xe dap", "Xe đạp"},
		{"punctuation splits", "64 gb", "iPhone 11 (64-GB)"},
		{"digits are words", "11", "iphone 11"},
		{"part of a word is not a word", "phone", "iphone 11"},
	} {
		id := catalogtest.Listing(t, e.db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.Title = c.text })
		cards, err := e.catalog.Search(ctx, domain.Search{
			Phrases: []domain.Phrase{{Text: c.phrase, Weight: 1, AllWords: true}},
			Filters: domain.Filters{IDs: []uuid.UUID{id}}, Sort: domain.SortNewest, Limit: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if index, ours := len(cards) == 1, domain.HoldsAllWords(c.phrase, c.text); index != ours {
			t.Errorf("%s: the index says %v, HoldsAllWords %v", c.name, index, ours)
		}
	}
}

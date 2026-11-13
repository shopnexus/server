package domain

import "slices"

// MinSoldForSuggestions is how many sold listings a sample needs before a
// guide says what to price at and how long it takes to sell.
const MinSoldForSuggestions = 3

type SuggestionKind string

const (
	SuggestFast   SuggestionKind = "fast"
	SuggestMarket SuggestionKind = "market"
)

// Suggestion is a price to sell at and the days a sale at it usually took.
type Suggestion struct {
	Kind  SuggestionKind `json:"kind"`
	Price int64          `json:"price"`
	Days  int            `json:"days"`
}

// Suggest is "Bán nhanh" and "Giá thị trường" from the samples' sold
// listings, none under MinSoldForSuggestions. The market price is est's
// median, at the median days of every sale. The fast price and days are
// the medians of the sales that took at most that long, left out unless
// the price is below the market's.
func Suggest(samples []Sample, est Estimate) []Suggestion {
	var sold []Sample
	for _, s := range samples {
		if s.Sold {
			sold = append(sold, s)
		}
	}
	if len(sold) < MinSoldForSuggestions {
		return []Suggestion{}
	}
	days := make([]int64, len(sold))
	for i, s := range sold {
		days[i] = int64(s.DaysToSell)
	}
	marketDays := median(days)
	// The quickest sale took at most the median, so the fast half is never
	// empty.
	var fastPrices, fastDays []int64
	for _, s := range sold {
		if int64(s.DaysToSell) <= marketDays {
			fastPrices = append(fastPrices, s.Price)
			fastDays = append(fastDays, int64(s.DaysToSell))
		}
	}
	out := []Suggestion{}
	if fast := roundPrice(median(fastPrices)); fast < est.Median {
		out = append(out, Suggestion{Kind: SuggestFast, Price: fast, Days: int(median(fastDays))})
	}
	return append(out, Suggestion{Kind: SuggestMarket, Price: est.Median, Days: int(marketDays)})
}

// median is the middle of xs, the mean of the two middle ones rounded down
// for an even count. xs is not empty.
func median(xs []int64) int64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

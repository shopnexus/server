package domain

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// MinMarketSample is how many similar listings a market range needs.
	MinMarketSample = 5
	// SoldWeight is what a sold listing counts in a market range; a listing
	// on sale counts 1.
	SoldWeight = 2
)

// Sample is the price of a similar listing, whether it sold, and the whole
// days its sale took (0 on sale).
type Sample struct {
	Price      int64
	Sold       bool
	DaysToSell int
}

// MarketEstimate is the weighted quartiles of the samples' prices, nil
// under MinMarketSample samples. Sorted by price, a sample weighs
// SoldWeight when sold and 1 otherwise; the q-quantile is the price of the
// first sample whose running weight reaches q of the total. Each is
// rounded to 10,000.
func MarketEstimate(samples []Sample) *Estimate {
	if len(samples) < MinMarketSample {
		return nil
	}
	s := slices.Clone(samples)
	slices.SortStableFunc(s, func(a, b Sample) int { return cmp.Compare(a.Price, b.Price) })
	var total int64
	for _, x := range s {
		total += sampleWeight(x)
	}
	// quartile k is the price where 4 × the running weight first reaches k
	// × the total.
	quartile := func(k int64) int64 {
		var run int64
		for _, x := range s {
			run += sampleWeight(x)
			if 4*run >= k*total {
				return roundPrice(x.Price)
			}
		}
		return roundPrice(s[len(s)-1].Price)
	}
	return &Estimate{Low: quartile(1), Median: quartile(2), High: quartile(3), Source: SourceMarket, SampleSize: len(s)}
}

func sampleWeight(s Sample) int64 {
	if s.Sold {
		return SoldWeight
	}
	return 1
}

// roundPrice rounds to the nearest 10,000 đồng, half up.
func roundPrice(v int64) int64 { return (v + 5_000) / 10_000 * 10_000 }

// cut keeps the first n characters of s, trimmed again after the cut.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

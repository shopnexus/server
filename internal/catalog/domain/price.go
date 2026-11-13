package domain

import (
	"slices"
	"time"
)

const (
	// PriceSampleSize is how many similar listings a price sample holds at
	// most.
	PriceSampleSize = 30
	// PriceSoldWindow and PriceOnSaleWindow are how far back a sold listing,
	// by its sale, and a listing on sale, by its publication, still count.
	PriceSoldWindow   = 180 * 24 * time.Hour
	PriceOnSaleWindow = 90 * 24 * time.Hour
)

// PriceSample is the price of one listing like a listing input, and whether it sold
// at it.
type PriceSample struct {
	Price int64 `json:"price"`
	Sold  bool  `json:"sold"`
}

// MedianPrice is the median of the samples' prices, the mean of the two
// middle ones rounded down for an even count; 0 without samples.
func MedianPrice(samples []PriceSample) int64 {
	if len(samples) == 0 {
		return 0
	}
	prices := make([]int64, len(samples))
	for i, s := range samples {
		prices[i] = s.Price
	}
	slices.Sort(prices)
	mid := len(prices) / 2
	if len(prices)%2 == 1 {
		return prices[mid]
	}
	return (prices[mid-1] + prices[mid]) / 2
}

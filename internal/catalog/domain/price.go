package domain

import "time"

const (
	// PriceSampleSize is how many similar listings a price sample holds at
	// most.
	PriceSampleSize = 30
	// PriceSoldWindow and PriceOnSaleWindow are how far back a sold listing,
	// by its sale, and a listing on sale, by its publication, still count.
	PriceSoldWindow   = 180 * 24 * time.Hour
	PriceOnSaleWindow = 90 * 24 * time.Hour
)

// PriceSample is the price of one listing like a draft, and whether it sold
// at it.
type PriceSample struct {
	Price int64 `json:"price"`
	Sold  bool  `json:"sold"`
}

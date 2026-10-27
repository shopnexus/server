package domain

import (
	"time"
	"uuid"
)

// MaxResults is how deep a search pages.
const MaxResults = 500

// SortOrder is how a search orders what it found.
type SortOrder string

const (
	SortRelevance SortOrder = "relevance"
	SortNewest    SortOrder = "newest"
	SortNearest   SortOrder = "nearest"
	SortPriceAsc  SortOrder = "price_asc"
	SortPriceDesc SortOrder = "price_desc"
)

// Filters are what the person picked: a listing outside them never shows.
type Filters struct {
	CategoryRoot, CategoryCode string
	PriceMin, PriceMax         int64 // 0 is no bound
	Conditions                 []Condition
	OffersOnly                 bool
	// Origin is where the person searches from; distance is measured from
	// it.
	Origin   *Point
	RadiusKm int // 0 is no radius
}

// Phrase is text to match in the title, the description and the specs. A
// negative weight demotes what matches.
type Phrase struct {
	Text   string
	Weight float64
}

// BoostKind is what a boost matches on.
type BoostKind string

const (
	BoostCategory     BoostKind = "category"
	BoostCategoryRoot BoostKind = "category_root"
	BoostPrice        BoostKind = "price"
	BoostCondition    BoostKind = "condition"
	BoostOffers       BoostKind = "offers"
	BoostNear         BoostKind = "near"
	BoostFresh        BoostKind = "fresh"
)

// Boost adds Weight to a listing's score when it matches, as a constant, so
// it moves listings that matched the phrases and brings in none. A negative
// weight demotes.
type Boost struct {
	Kind               BoostKind
	Weight             float64
	Code               string    // category, category_root
	PriceMin, PriceMax int64     // price; 0 is open
	Condition          Condition // condition
	Near               Point     // near
	RadiusKm           float64   // near
	Since              time.Time // fresh: published at or after
}

// Search is a page of listings: the person's filters, the phrases that must
// match one at least, the boosts that order what matched, and the sort.
// Without phrases it browses every active listing under the filters.
type Search struct {
	Filters       Filters
	Phrases       []Phrase
	Boosts        []Boost
	Sort          SortOrder
	Offset, Limit int
}

// Hit is a listing a search found, with its point for the distance.
type Hit struct {
	ID    uuid.UUID
	Point Point
}

// CountField is what Counts groups by.
type CountField string

const (
	CountCode CountField = "category_code"
	CountRoot CountField = "category_root"
)

// CategoryCount is how many listings a search matches in one category.
type CategoryCount struct {
	Code  string `json:"category"`
	Count int    `json:"count"`
}

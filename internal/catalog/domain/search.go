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
	// SortBlend interleaves streams, each newest first: one per category
	// boost and one for the listings outside them, which weighs as much as
	// the heaviest boost. A stream's share of every page follows its
	// weight; a tie goes newest first, then by ID. A boost of another kind
	// is an error. No endpoint takes it; discovery's Dành cho bạn feed uses
	// it.
	SortBlend SortOrder = "blend"
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
	// Province keeps the listings whose commune is in it, empty for every
	// province.
	Province string
	// Seller keeps one seller's listings, zero for every seller.
	Seller uuid.UUID
	// IDs keeps the search to these listings, empty for every listing: a
	// watch's sweep weighs the listings that arrived since the last one.
	IDs []uuid.UUID
}

// Phrase is text to match in the title, the description and the specs. A
// negative weight demotes what matches. AllWords makes it match only a
// listing holding every word of Text in one of the three, as a watch's
// sweep needs; a ranked phrase matches any of its words. Folded matches the
// three with diacritics folded away, as a person's raw words need: "xe dap"
// then matches "Xe đạp".
type Phrase struct {
	Text     string
	Weight   float64
	AllWords bool
	Folded   bool
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
	// Vectors and Weights make the search hybrid: each list (BM25 on the
	// phrases, text vectors, photo vectors, boosts) takes listings under the
	// filters, and RRF fuses them. Sorted by relevance a list reads a few
	// times the page's end; other sorts order the fused listings, so their
	// lists read MaxResults.
	Vectors Vectors
	Weights Weights
	// Like is the source of a similar-listings search, zero for none: the
	// service reads its vectors into Vectors, and the search leaves it out.
	Like uuid.UUID
	// ExcludeListings and ExcludeSellers leave listings out: the ones the
	// viewer hid, the sellers on either side of the viewer's blocks, and the
	// viewer's own in a similar-listings search or a sweep.
	ExcludeListings, ExcludeSellers []uuid.UUID
}

// Hybrid reports whether s fuses lists; otherwise it is one BM25 query.
func (s Search) Hybrid() bool { return s.Weights != Weights{} }

// Hit is a listing a search found, with its point for the distance.
type Hit struct {
	ID    uuid.UUID
	Point Point
}

// RRFK is the k of reciprocal rank fusion: a list's share for the listing at
// rank r is its weight / (RRFK + r).
const RRFK = 60

// Vectors are the query vectors of a hybrid search. A nil vector leaves its
// list out. A listing joins the text list at TextMin cosine or more, and the
// photo list when its closest photo is at PhotoMin or more; only vectors of
// the configured versions are read.
type Vectors struct {
	Text, Photo               []float32
	TextVersion, PhotoVersion string
	TextMin, PhotoMin         float64
}

// Weights are each list's weight in the fusion; a zero leaves the list out.
// Boost is the list that ranks the other lists' listings by boost points
// alone, so a boost moves listings a vector list found.
type Weights struct {
	BM25, Text, Photo, Boost float64
}

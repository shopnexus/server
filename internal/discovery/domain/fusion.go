package domain

import catalogdomain "github.com/shopnexus/server/internal/catalog/domain"

// The weights of each list in a hybrid search's RRF, starting values that
// make search-eval tunes (see ARCHITECTURE.md, Search).
var (
	FuseQuery = catalogdomain.Weights{BM25: 1, Text: 1, Photo: 0.5, Boost: 0.75}
	FuseLike  = catalogdomain.Weights{BM25: 0.5, Text: 1, Photo: 1, Boost: 0.5}
	FusePhoto = catalogdomain.Weights{Photo: 1}
)

// LikePriceBand is the share of the source's price either side of it that a
// similar listing's price is boosted within.
const LikePriceBand = 0.3

// LikeTerms are the BM25 terms of a similar-listings search: the source's
// title as a phrase, and boosts for its subcategory and for prices within
// LikePriceBand of its own, at the model's category and price weights.
func LikeTerms(title, category string, price int64) ([]catalogdomain.Phrase, []catalogdomain.Boost) {
	band := int64(float64(price) * LikePriceBand)
	return []catalogdomain.Phrase{{Text: title, Weight: weight[AttrPhrase]}},
		[]catalogdomain.Boost{
			{Kind: catalogdomain.BoostCategory, Code: category, Weight: weight[AttrCategory]},
			{Kind: catalogdomain.BoostPrice, PriceMin: price - band, PriceMax: price + band, Weight: weight[AttrPrice]},
		}
}

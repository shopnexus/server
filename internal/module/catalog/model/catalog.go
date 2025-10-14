package catalogmodel

import (
	"shopnexus-remastered/internal/db"
)

const (
	CacheRecommendSize       = 100
	CacheKeyRecommendProduct = "catalog:recommend:product:%d"
	CacheKeyRecommendOffset  = "catalog:recommend:offset:%d"
)

// ProductPrice is the final price of a product SKU after applying promotions
type ProductPrice struct {
	SkuID         int64
	Price         int64
	OriginalPrice int64
	Promotions    []db.PromotionBase
}

type Rating struct {
	Score float32 `json:"score"`
	Total int     `json:"total"`
}

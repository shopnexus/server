package catalogmodel

import (
	commonmodel "shopnexus-remastered/internal/module/common/model"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
)

const (
	CacheRecommendSize       = 100
	CacheKeyRecommendProduct = "catalog:recommend:product:%d"
	CacheKeyRecommendOffset  = "catalog:recommend:offset:%d"
)

// ProductPrice is the final price of a product SKU after applying promotions
type ProductPrice struct {
	SkuID         int64
	Price         commonmodel.Concurrency
	OriginalPrice commonmodel.Concurrency
	Promotions    []promotionmodel.PromotionBase
}

type Rating struct {
	Score float32 `json:"score"`
	Total int     `json:"total"`
}

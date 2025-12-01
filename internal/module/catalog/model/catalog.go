package catalogmodel

import (
	commonmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
)

const (
	CacheRecommendSize       = 100
	CacheKeyRecommendProduct = "catalog:recommend:product:%d"
	CacheKeyRecommendOffset  = "catalog:recommend:offset:%d"
)

// OrderPrice is the final price of a order after applying promotions
type OrderPrice struct {
	Request RequestOrderPrice

	ProductCost commonmodel.Concurrency
	ShipCost    commonmodel.Concurrency

	PromotionCodes []string
}

func (o *OrderPrice) Total() commonmodel.Concurrency {
	return o.ProductCost.Add(o.ShipCost)
}

type RequestOrderPrice struct {
	SkuID          uuid.UUID
	SpuID          uuid.UUID
	UnitPrice      commonmodel.Concurrency
	Quantity       int64
	ShipCost       commonmodel.Concurrency
	PromotionCodes []string
}

func (ro RequestOrderPrice) ProductCost() commonmodel.Concurrency {
	return ro.UnitPrice.Mul(ro.Quantity)
}

func (ro RequestOrderPrice) Total() commonmodel.Concurrency {
	return ro.ProductCost().Add(ro.ShipCost)
}

type Rating struct {
	Score float32 `json:"score"`
	Total int     `json:"total"`
}

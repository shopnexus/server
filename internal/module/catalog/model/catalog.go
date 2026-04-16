package catalogmodel

import (
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

const (
	CacheRecommendSize       = 100
	CacheKeyRecommendProduct = "catalog:recommend:product:%s"
	CacheKeyRecommendOffset  = "catalog:recommend:offset:%s"
)

// OrderPrice is the final price of a order after applying promotions.
type OrderPrice struct {
	Request RequestOrderPrice

	ProductCost sharedmodel.Concurrency
	ShipCost    sharedmodel.Concurrency

	PromotionCodes []string
}

func (o *OrderPrice) Total() sharedmodel.Concurrency {
	return o.ProductCost.Add(o.ShipCost)
}

type RequestOrderPrice struct {
	SkuID          uuid.UUID
	SpuID          uuid.UUID
	UnitPrice      sharedmodel.Concurrency
	Quantity       int64
	ShipCost       sharedmodel.Concurrency
	PromotionCodes []string
}

func (ro RequestOrderPrice) ProductCost() sharedmodel.Concurrency {
	return ro.UnitPrice.Mul(ro.Quantity)
}

func (ro RequestOrderPrice) Total() sharedmodel.Concurrency {
	return ro.ProductCost().Add(ro.ShipCost)
}

type Rating struct {
	Score float32 `json:"score"`
	Total int     `json:"total"`
}

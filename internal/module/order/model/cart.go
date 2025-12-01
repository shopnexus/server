package ordermodel

import (
	commonmodel "shopnexus-remastered/internal/module/common/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
)

// TODO: should embed the product card data
type CheckoutSku struct {
	SkuID         uuid.UUID               `json:"sku_id"`
	SpuID         uuid.UUID               `json:"spu_id"`
	Name          string                  `json:"name"`
	SkuName       string                  `json:"sku_name"`
	OriginalPrice sharedmodel.Concurrency `json:"original_price"`
	Price         sharedmodel.Concurrency `json:"price"`
	Quantity      int64                   `json:"quantity"`
	Resources     []commonmodel.Resource  `json:"resources"`
	Category      string                  `json:"category"`
	Promotions    []string                `json:"promotions"`
}

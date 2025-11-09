package accountmodel

import commonmodel "shopnexus-remastered/internal/module/common/model"

// TODO: should embed the product card data
type CheckoutSku struct {
	SkuID         int64                   `json:"sku_id"`
	SpuID         int64                   `json:"spu_id"`
	Name          string                  `json:"name"`
	SkuName       string                  `json:"sku_name"`
	OriginalPrice commonmodel.Concurrency `json:"original_price"`
	Price         commonmodel.Concurrency `json:"price"`
	Quantity      int64                   `json:"quantity"`
	Resources     []commonmodel.Resource  `json:"resources"`
	Category      string                  `json:"category"`
	Promotions    []int64                 `json:"promotions"`
}

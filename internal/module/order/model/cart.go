package ordermodel

import (
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonmodel "shopnexus-server/internal/module/common/model"

	"github.com/google/uuid"
)

type CartItem struct {
	SpuID    uuid.UUID               `json:"spu_id"`
	Sku      catalogmodel.ProductSku `json:"sku"`
	Quantity int64                   `json:"quantity"`
	Resource *commonmodel.Resource   `json:"resource"`
	Currency string                  `json:"currency"` // ISO 4217 from SPU
}

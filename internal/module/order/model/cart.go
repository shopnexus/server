package ordermodel

import (
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"

	"github.com/google/uuid"
)

type CartItem struct {
	SpuID    uuid.UUID               `json:"spu_id"`
	Sku      catalogmodel.ProductSku `json:"sku"`
	Quantity int64                   `json:"quantity"`
	Resource *commonmodel.Resource   `json:"resource"`
}

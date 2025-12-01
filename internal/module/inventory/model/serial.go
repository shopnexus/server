package inventorymodel

import (
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	"time"

	"github.com/google/uuid"
)

type ProductSerial struct {
	ID          string                             `json:"id"`
	SkuID       uuid.UUID                          `json:"sku_id"`
	Status      inventorydb.InventoryProductStatus `json:"status"`
	DateCreated time.Time                          `json:"date_created"`
}

package inventorymodel

import (
	"shopnexus-remastered/internal/db"
	"time"
)

type ProductSerial struct {
	ID           int64                     `json:"id"`
	SerialNumber string                    `json:"serial_number"`
	SkuID        int64                     `json:"sku_id"`
	Status       db.InventoryProductStatus `json:"status"`
	DateCreated  time.Time                 `json:"date_created"`
}

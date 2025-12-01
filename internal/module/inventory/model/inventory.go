package inventorymodel

import (
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	"time"

	"github.com/google/uuid"
)

const (
	TopicInventoryStockUpdated = "inventory.stock.updated"
)

type Stock struct {
	ID          int64                             `json:"id"`
	RefType     inventorydb.InventoryStockRefType `json:"ref_type"`
	RefID       uuid.UUID                         `json:"ref_id"`
	Stock       int64                             `json:"stock"`
	Taken       int64                             `json:"taken"`
	DateCreated time.Time                         `json:"date_created"`
}

type StockHistory struct {
	ID          int64     `json:"id"`
	Change      int64     `json:"change"`
	DateCreated time.Time `json:"date_created"`
}

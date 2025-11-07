package inventorymodel

import (
	"time"

	"shopnexus-remastered/internal/db"
)

const (
	TopicInventoryStockUpdated = "inventory.stock.updated"
)

type Stock struct {
	ID           int64                    `json:"id"`
	RefType      db.InventoryStockRefType `json:"ref_type"`
	RefID        int64                    `json:"ref_id"`
	CurrentStock int64                    `json:"current_stock"`
	Sold         int64                    `json:"sold"`
	DateCreated  time.Time                `json:"date_created"`
}

type StockHistory struct {
	ID          int64     `json:"id"`
	Change      int64     `json:"change"`
	DateCreated time.Time `json:"date_created"`
}

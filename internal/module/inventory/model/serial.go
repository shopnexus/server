package inventorymodel

import (
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	"time"

	"github.com/google/uuid"
)

type Serial struct {
	ID          string                            `json:"id"`
	RefType     inventorydb.InventoryStockRefType `json:"ref_type"`
	RefID       uuid.UUID                         `json:"ref_id"`
	Status      inventorydb.InventoryStatus       `json:"status"`
	DateCreated time.Time                         `json:"date_created"`
}

package inventorymodel

import sharedmodel "shopnexus-server/internal/shared/model"

// Sentinel errors for the inventory module.
var (
	ErrOutOfStock          = sharedmodel.NewError("inventory.out_of_stock", "Sorry, This product (%s) is out of stock right now")
	ErrSerialCountMismatch = sharedmodel.NewError("inventory.serial_count_mismatch", "The number of serial IDs must match the quantity")
)

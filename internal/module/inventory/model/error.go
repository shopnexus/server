package inventorymodel

import sharedmodel "shopnexus-server/internal/shared/model"

var (
	ErrOutOfStock = sharedmodel.NewError("inventory.out_of_stock", "Sorry, This product (%s) is out of stock right now")
)

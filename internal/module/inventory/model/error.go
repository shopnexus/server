package inventorymodel

import sharedmodel "shopnexus-remastered/internal/shared/model"

var (
	ErrOutOfStock = sharedmodel.NewError("inventory.out_of_stock", "Sorry, product \"%s\" is out of stock right now")
)

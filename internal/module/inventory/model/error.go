package inventorymodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the inventory module.
var (
	ErrOutOfStock          = sharedmodel.NewError(http.StatusConflict, "Sorry, This product (%s) is out of stock right now")
	ErrSerialCountMismatch = sharedmodel.NewError(http.StatusBadRequest, "The number of serial IDs must match the quantity")
)

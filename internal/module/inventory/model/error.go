package inventorymodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the inventory module.
var (
	ErrSerialCountMismatch = sharedmodel.NewError(
		http.StatusBadRequest,
		"serial_count_mismatch",
		"The number of serial IDs must match the quantity",
	)
	ErrInsufficientReservedInventory = sharedmodel.NewError(
		http.StatusConflict,
		"insufficient_reserved_inventory",
		"insufficient reserved inventory to release",
	)
	ErrOutOfStock = sharedmodel.NewError(
		http.StatusConflict,
		"out_of_stock",
		"Sorry, this %s is out of stock right now (requested %d, only %d available)",
	)
	ErrOutOfStockRace = sharedmodel.NewError(
		http.StatusConflict,
		"out_of_stock_race",
		"This %s was just reserved by someone else. Please try again.",
	)
	ErrSerialShortage = sharedmodel.NewError(
		http.StatusConflict,
		"serial_shortage",
		"Only %d unit(s) of this %s have a serial available (requested %d)",
	)
)

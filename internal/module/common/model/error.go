package commonmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the common module.
var (
	ErrResourceNotFound         = sharedmodel.NewError(http.StatusNotFound, "resource_not_found", "Resource not found")
	ErrEmptyAddress             = sharedmodel.NewError(http.StatusBadRequest, "empty_address", "address is empty")
	ErrAddressCountryUnresolved = sharedmodel.NewError(
		http.StatusBadRequest,
		"address_country_unresolved",
		"could not verify address country (no country in geocode result)",
	)
)

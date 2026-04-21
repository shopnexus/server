package commonmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the common module.
var (
	ErrResourceNotFound = sharedmodel.NewError(http.StatusNotFound, "resource_not_found", "Resource not found")
)

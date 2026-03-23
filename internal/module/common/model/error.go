package commonmodel

import sharedmodel "shopnexus-server/internal/shared/model"

// Sentinel errors for the common module.
var (
	ErrResourceNotFound = sharedmodel.NewError("resource.not_found", "Resource not found")
)

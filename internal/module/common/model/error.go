package commonmodel

import sharedmodel "shopnexus-server/internal/shared/model"

var (
	ErrResourceNotFound = sharedmodel.NewError("resource.not_found", "Resource not found")
)

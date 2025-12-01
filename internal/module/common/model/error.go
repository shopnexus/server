package commonmodel

import sharedmodel "shopnexus-remastered/internal/shared/model"

var (
	ErrResourceNotFound = sharedmodel.NewError("resource.not_found", "Resource not found")
)

package catalogmodel

import (
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
)

type Comment struct {
	db.CatalogComment
	Resources []sharedmodel.Resource `json:"resources"`
}

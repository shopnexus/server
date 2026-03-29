package catalogmodel

import (
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

type ProductDetail struct {
	ID             uuid.UUID                 `json:"id"`
	Slug           string                    `json:"slug"`
	VendorID       uuid.UUID                 `json:"vendor_id"`
	Name           string                    `json:"name"`
	Description    string                    `json:"description"`
	IsActive       bool                      `json:"is_active"`
	Category       catalogdb.CatalogCategory `json:"category"`
	Rating         ProductRating             `json:"rating"`
	IsFavorite     bool                      `json:"is_favorite"`
	Resources      []commonmodel.Resource    `json:"resources"`
	Promotions     []ProductCardPromo        `json:"promotions"`
	Skus           []ProductDetailSku        `json:"skus"`
	Specifications []ProductSpecification    `json:"specifications"`
	Tags           []string                  `json:"tags"`
}

type ProductRating struct {
	Score     float64     `json:"score"`
	Total     int64       `json:"total"`
	Breakdown map[int]int `json:"breakdown"`
}

type ProductDetailSku struct {
	ID            uuid.UUID               `json:"id"`
	Price         sharedmodel.Concurrency `json:"price"`
	OriginalPrice sharedmodel.Concurrency `json:"original_price"`
	Attributes    []ProductAttribute      `json:"attributes"`
	Taken         int64                   `json:"taken"`
	Stock         int64                   `json:"stock"`
}

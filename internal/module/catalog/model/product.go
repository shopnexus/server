package catalogmodel

import (
	"encoding/json"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"time"

	"github.com/google/uuid"
)

type ProductSpu struct {
	ID            uuid.UUID                 `json:"id"`
	AccountID     uuid.UUID                 `json:"account_id"`
	Slug          string                    `json:"slug"`
	Category      catalogdb.CatalogCategory `json:"category"`
	Brand         catalogdb.CatalogBrand    `json:"brand"`
	FeaturedSkuID uuid.NullUUID             `json:"featured_sku_id"`
	Name          string                    `json:"name"`
	Description   string                    `json:"description"`
	IsActive      bool                      `json:"is_active"`
	DateCreated   time.Time                 `json:"date_created"`
	DateUpdated   time.Time                 `json:"date_updated"`

	Rating         ProductRating          `json:"rating"`
	Tags           []string               `json:"tags"`
	Resources      []commonmodel.Resource `json:"resources"`
	Specifications []ProductSpecification `json:"specifications"`
}

type ProductSku struct {
	ID          uuid.UUID               `json:"id"`
	SpuID       uuid.UUID               `json:"spu_id"`
	Price       sharedmodel.Concurrency `json:"price"`
	CanCombine  bool                    `json:"can_combine"`
	DateCreated time.Time               `json:"date_created"`
	Stock       int64                   `json:"stock"`

	Attributes     []ProductAttribute `json:"attributes"`
	PackageDetails json.RawMessage    `json:"package_details"`
}

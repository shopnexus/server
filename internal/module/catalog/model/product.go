package catalogmodel

import (
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"time"

	"github.com/guregu/null/v6"
)

type ProductSpu struct {
	ID            int64              `json:"id"`
	Code          string             `json:"code"`
	Category      db.CatalogCategory `json:"category"`
	Brand         db.CatalogBrand    `json:"brand"`
	FeaturedSkuID null.Int64         `json:"featured_sku_id"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	IsActive      bool               `json:"is_active"`
	DateCreated   time.Time          `json:"date_created"`
	DateUpdated   time.Time          `json:"date_updated"`

	Rating    ProductRating          `json:"rating"`
	Tags      []string               `json:"tags"`
	Resources []sharedmodel.Resource `json:"resources"`
}

type ProductSku struct {
	ID          int64     `json:"id"`
	SpuID       int64     `json:"spu_id"`
	Price       int64     `json:"price"`
	CanCombine  bool      `json:"can_combine"`
	DateCreated time.Time `json:"date_created"`
	Stock       int64     `json:"stock"`

	Attributes []ProductAttribute `json:"attributes"`
}

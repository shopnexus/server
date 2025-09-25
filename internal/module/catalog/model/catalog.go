package catalogmodel

import (
	"shopnexus-remastered/internal/db"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	CacheRecommendSize       = 100
	CacheKeyRecommendProduct = "catalog:recommend:product:%d"
	CacheKeyRecommendOffset  = "catalog:recommend:offset:%d"
)

type ProductSku struct {
	ID          int64              `json:"id"`
	Code        string             `json:"code"`
	SpuID       int64              `json:"spu_id"`
	Price       int64              `json:"price"`
	CanCombine  bool               `json:"can_combine"`
	DateCreated pgtype.Timestamptz `json:"date_created"`
	DateDeleted pgtype.Timestamptz `json:"date_deleted"`

	Attributes []ProductSkuAttribute `json:"attributes"`
}

type ProductSkuAttribute struct {
	ID          int64              `json:"id"`
	Code        string             `json:"code"`
	SkuID       int64              `json:"sku_id"`
	Name        string             `json:"name"`
	Value       string             `json:"value"`
	DateCreated pgtype.Timestamptz `json:"date_created"`
	DateUpdated pgtype.Timestamptz `json:"date_updated"`
}

// ProductPrice is the final price of a product SKU after applying promotions
type ProductPrice struct {
	SkuID         int64
	Price         int64
	OriginalPrice int64
	Promotions    []db.PromotionBase
}

type Rating struct {
	Score float32 `json:"score"`
	Total int     `json:"total"`
}

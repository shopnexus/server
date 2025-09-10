package catalogmodel

import (
	"shopnexus-remastered/internal/db"

	"github.com/jackc/pgx/v5/pgtype"
)

type ProductDetail struct {
	ID             int64              `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	Images         []string           `json:"images"`
	Category       string             `json:"category"`
	Rating         RatingDetail       `json:"rating"`
	Sold           int                `json:"sold"`
	Promotions     []ProductCardPromo `json:"promotions"`
	Skus           []SkuDetail        `json:"skus"`
	Specifications map[string]string  `json:"specifications"`
}

type RatingDetail struct {
	Score     float64     `json:"score"`
	Total     int64       `json:"total"`
	Breakdown map[int]int `json:"breakdown"`
}

type SkuDetail struct {
	ID            int64             `json:"id"`
	Price         int64             `json:"price"`
	OriginalPrice int64             `json:"original_price"`
	Attributes    map[string]string `json:"attributes"`
}

type ProductCard struct {
	ID               int64              `json:"id"`
	Code             string             `json:"code"`
	VendorID         int64              `json:"vendor_id"`
	CategoryID       int64              `json:"category_id"`
	BrandID          int64              `json:"brand_id"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	IsActive         bool               `json:"is_active"`
	DateManufactured pgtype.Timestamptz `json:"date_manufactured"`
	DateCreated      pgtype.Timestamptz `json:"date_created"`
	DateUpdated      pgtype.Timestamptz `json:"date_updated"`
	DateDeleted      pgtype.Timestamptz `json:"date_deleted"`

	Price         int64              `json:"price"`
	OriginalPrice int64              `json:"original_price"`
	Rating        Rating             `json:"rating"`
	Image         string             `json:"image"`
	Promotions    []ProductCardPromo `json:"promotions"`
}

type ProductCardPromo struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

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

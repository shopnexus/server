package catalogmodel

import (
	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
)

type ProductDetail struct {
	ID             int64                  `json:"id"`
	Code           string                 `json:"code"`
	VendorID       int64                  `json:"vendor_id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Brand          db.CatalogBrand        `json:"brand"`
	IsActive       bool                   `json:"is_active"`
	Category       db.CatalogCategory     `json:"category"`
	Rating         ProductRating          `json:"rating"`
	Resources      []commonmodel.Resource `json:"resources"`
	Promotions     []ProductCardPromo     `json:"promotions"`
	Skus           []ProductDetailSku     `json:"skus"`
	Specifications map[string]string      `json:"specifications"`
}

type ProductRating struct {
	Score     float64     `json:"score"`
	Total     int64       `json:"total"`
	Breakdown map[int]int `json:"breakdown"`
}

type ProductDetailSku struct {
	ID            int64              `json:"id"`
	Price         int64              `json:"price"`
	OriginalPrice int64              `json:"original_price"`
	Attributes    []ProductAttribute `json:"attributes"`
	Sold          int64              `json:"sold"`
}

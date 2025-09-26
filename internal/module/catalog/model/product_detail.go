package catalogmodel

import sharedmodel "shopnexus-remastered/internal/module/shared/model"

type ProductDetail struct {
	ID             int64                  `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Brand          string                 `json:"brand"`
	IsActive       bool                   `json:"is_active"`
	Category       string                 `json:"category"`
	Rating         ProductDetailRating    `json:"rating"`
	Resources      []sharedmodel.Resource `json:"resources"`
	Promotions     []ProductCardPromo     `json:"promotions"`
	Skus           []ProductDetailSku     `json:"skus"`
	Specifications map[string]string      `json:"specifications"`
}

type ProductDetailRating struct {
	Score     float64     `json:"score"`
	Total     int64       `json:"total"`
	Breakdown map[int]int `json:"breakdown"`
}

type ProductDetailSku struct {
	ID            int64             `json:"id"`
	Price         int64             `json:"price"`
	OriginalPrice int64             `json:"original_price"`
	Attributes    map[string]string `json:"attributes"`
	Sold          int64             `json:"sold"`
}

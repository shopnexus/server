package catalogmodel

import (
	"time"

	commonmodel "shopnexus-remastered/internal/module/common/model"
)

type ProductCard struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	VendorID    int64     `json:"vendor_id"`
	CategoryID  int64     `json:"category_id"`
	BrandID     int64     `json:"brand_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`
	DateDeleted time.Time `json:"date_deleted"`

	Price         commonmodel.Concurrency `json:"price"`
	OriginalPrice commonmodel.Concurrency `json:"original_price"`
	Rating        Rating                  `json:"rating"`
	Resources     []commonmodel.Resource  `json:"resources"`
	Promotions    []ProductCardPromo      `json:"promotions"`
}

type ProductCardPromo struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

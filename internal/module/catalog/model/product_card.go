package catalogmodel

import (
	"time"

	sharedmodel "shopnexus-remastered/internal/module/shared/model"
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

	Price         sharedmodel.Concurrency `json:"price"`
	OriginalPrice sharedmodel.Concurrency `json:"original_price"`
	Rating        Rating                  `json:"rating"`
	Resource      sharedmodel.Resource    `json:"resource"`
	Promotions    []ProductCardPromo      `json:"promotions"`
}

type ProductCardPromo struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

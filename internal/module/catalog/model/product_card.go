package catalogmodel

import "github.com/jackc/pgx/v5/pgtype"

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

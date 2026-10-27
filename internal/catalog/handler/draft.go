package handler

import (
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// draftBody is the ListingDraft schema in api/schemas/catalog.yaml.
type draftBody struct {
	Title        string           `json:"title"`
	Price        int64            `json:"price"`
	Description  string           `json:"description"`
	CategoryCode string           `json:"category_code"`
	Condition    domain.Condition `json:"condition"`
	AllowOffers  bool             `json:"allow_offers"`
	CommuneCode  string           `json:"commune_code"`
	Photos       []uuid.UUID      `json:"photos"`
	Specs        []domain.Spec    `json:"specs"`
	GPS          *gpsBody         `json:"gps"`
}

// gpsBody is the seller's position. The handler blurs it at once, so the
// exact point goes no further than this request.
type gpsBody struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (b *draftBody) draft() domain.Draft {
	var spot *domain.Point
	if b.GPS != nil {
		spot = new(domain.Blur(domain.Point{Lat: b.GPS.Lat, Lng: b.GPS.Lng}))
	}
	return domain.Draft{
		Title: b.Title, Price: b.Price, Description: b.Description, CategoryCode: b.CategoryCode,
		Condition: b.Condition, AllowOffers: b.AllowOffers, CommuneCode: b.CommuneCode,
		Photos: b.Photos, Specs: b.Specs, Spot: spot,
	}
}

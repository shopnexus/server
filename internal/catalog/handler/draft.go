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
	Marks        []markBody       `json:"marks"`
	Specs        []domain.Spec    `json:"specs"`
}

type markBody struct {
	Text     string     `json:"text"`
	UploadID *uuid.UUID `json:"upload_id"`
}

func (b *draftBody) draft() domain.Draft {
	marks := make([]domain.MarkDraft, 0, len(b.Marks))
	for _, m := range b.Marks {
		marks = append(marks, domain.MarkDraft{Text: m.Text, Photo: m.UploadID})
	}
	return domain.Draft{
		Title: b.Title, Price: b.Price, Description: b.Description, CategoryCode: b.CategoryCode,
		Condition: b.Condition, AllowOffers: b.AllowOffers, CommuneCode: b.CommuneCode,
		Photos: b.Photos, Marks: marks, Specs: b.Specs,
	}
}

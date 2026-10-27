package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
)

// Pane is one of the seller's lists of their own listings.
type Pane string

const (
	// PaneOnSale holds listings in processing, active, hidden and rejected.
	PaneOnSale Pane = "on_sale"
	PaneSold   Pane = "sold"
)

// ListingView is the Listing schema in api/schemas/catalog.yaml.
type ListingView struct {
	ID           uuid.UUID        `json:"id"`
	SellerID     uuid.UUID        `json:"seller_id"`
	Title        string           `json:"title"`
	Price        int64            `json:"price"`
	Description  string           `json:"description"`
	CategoryCode string           `json:"category_code"`
	Condition    domain.Condition `json:"condition"`
	AllowOffers  bool             `json:"allow_offers"`
	Location     Location         `json:"location"`
	Photos       []PhotoView      `json:"photos"`
	Marks        []MarkView       `json:"marks"`
	Specs        []domain.Spec    `json:"specs"`
	Status       domain.Status    `json:"status"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	PublishedAt  *time.Time       `json:"published_at,omitempty"`
	SoldAt       *time.Time       `json:"sold_at,omitempty"`
}

// Summary is what another module reads of a listing, in any status.
// CoverThumbURL is empty until the cover is processed.
// ReservedFor is the order a reserved listing is held for. A flow of
// module deal journals a Summary, hence the JSON tags.
type Summary struct {
	ID            uuid.UUID     `json:"id"`
	SellerID      uuid.UUID     `json:"seller_id"`
	Title         string        `json:"title"`
	Price         int64         `json:"price"`
	AllowOffers   bool          `json:"allow_offers"`
	Status        domain.Status `json:"status"`
	CoverThumbURL string        `json:"cover_thumb_url"`
	ReservedFor   uuid.UUID     `json:"reserved_for"`
}

// Location is the listing's commune and province, with a point inside the
// commune rather than where the seller is.
type Location struct {
	Commune  Region  `json:"commune"`
	Province Region  `json:"province"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// Region is a commune or a province as a listing names it.
type Region struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
}

// PhotoView is a photo of the listing. URL and ThumbURL are set once it is
// processed; Failure is why processing refused it, on a rejected listing.
type PhotoView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url,omitempty"`
	ThumbURL string    `json:"thumb_url,omitempty"`
	Failure  string    `json:"failure,omitempty"`
}

type MarkView struct {
	Text  string     `json:"text"`
	Photo *PhotoView `json:"photo,omitempty"`
}

// view builds the ListingView of l.
func (c *Catalog) view(ctx context.Context, l domain.Listing) (ListingView, error) {
	views, err := c.views(ctx, []domain.Listing{l})
	if err != nil {
		return ListingView{}, err
	}
	return views[0], nil
}

// views builds the ListingViews of ls, reading their communes in one query.
func (c *Catalog) views(ctx context.Context, ls []domain.Listing) ([]ListingView, error) {
	codes := make([]string, 0, len(ls))
	for i := range ls {
		codes = append(codes, ls[i].CommuneCode())
	}
	places, err := c.Places(ctx, codes)
	if err != nil {
		return nil, err
	}
	out := make([]ListingView, 0, len(ls))
	for i := range ls {
		l := &ls[i]
		photo := func(p domain.Photo) PhotoView {
			v := PhotoView{UploadID: p.UploadID, Failure: l.Rejection()[p.UploadID]}
			if p.Keys.Photo != "" {
				v.URL, v.ThumbURL = c.media.URL(p.Keys.Photo), c.media.URL(p.Keys.Thumb)
			}
			return v
		}
		photos := make([]PhotoView, 0, len(l.Photos()))
		for _, p := range l.Photos() {
			photos = append(photos, photo(p))
		}
		marks := make([]MarkView, 0, len(l.Marks()))
		for _, m := range l.Marks() {
			mv := MarkView{Text: m.Text}
			if m.Photo != nil {
				mv.Photo = new(photo(*m.Photo))
			}
			marks = append(marks, mv)
		}
		out = append(out, ListingView{
			ID: l.ID(), SellerID: l.SellerID(), Title: l.Title(), Price: l.Price(), Description: l.Description(),
			CategoryCode: l.CategoryCode(), Condition: l.Condition(), AllowOffers: l.AllowOffers(),
			Location: places[l.CommuneCode()],
			Photos:   photos, Marks: marks, Specs: l.Specs(), Status: l.Status(),
			CreatedAt: l.CreatedAt(), UpdatedAt: l.UpdatedAt(), PublishedAt: l.PublishedAt(), SoldAt: l.SoldAt(),
		})
	}
	return out, nil
}

// own loads the seller's listing id. Another seller's listing is
// domain.ErrNotFound, so a listing's existence does not leak; a deleted one
// is the seller's still, and the domain refuses to change it.
func (c *Catalog) own(ctx context.Context, seller, id uuid.UUID) (domain.Listing, error) {
	l, err := c.listings.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Listing{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Listing{}, fmt.Errorf("get listing: %w", err)
	}
	if l.SellerID() != seller {
		return domain.Listing{}, domain.ErrNotFound
	}
	return l, nil
}

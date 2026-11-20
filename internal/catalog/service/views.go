package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
)

// Pane is one of the seller's lists of their own listings.
type Pane string

const (
	// PaneOnSale holds listings in processing, active, hidden, rejected and
	// reserved: domain.OnSale.
	PaneOnSale Pane = "on_sale"
	PaneSold   Pane = "sold"
)

// ListingView is the Listing schema in api/schemas/catalog.yaml.
type ListingView struct {
	ID           uuid.UUID         `json:"id"`
	Slug         string            `json:"slug"`
	SellerID     uuid.UUID         `json:"seller_id"`
	Title        string            `json:"title"`
	Price        int64             `json:"price"`
	Description  string            `json:"description"`
	CategoryCode string            `json:"category_code"`
	Condition    domain.Condition  `json:"condition"`
	AllowOffers  bool              `json:"allow_offers"`
	Location     Location          `json:"location"`
	Photos       []PhotoView       `json:"photos"`
	Specs        []domain.Spec     `json:"specs"`
	Status       domain.Status     `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	PublishedAt  *time.Time        `json:"published_at,omitempty"`
	SoldAt       *time.Time        `json:"sold_at,omitempty"`
	PriceDrop    *domain.PriceDrop `json:"price_drop,omitempty"`
}

// ReportedListing is the ReportedListing schema in api/schemas/console.yaml:
// a listing as console members see it beside a report case. PhotoURL is the
// cover's thumbnail, empty until the cover is processed.
type ReportedListing struct {
	ID        uuid.UUID     `json:"id"`
	Title     string        `json:"title"`
	Price     int64         `json:"price"`
	Status    domain.Status `json:"status"`
	PhotoURL  string        `json:"photo_url,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
}

// Summary is what another module reads of a listing, in any status.
// CoverThumbURL is empty until the cover is processed.
// ReservedFor is the order a reserved listing is held for. A flow of
// module deal journals a Summary, hence the JSON tags.
type Summary struct {
	ID            uuid.UUID     `json:"id"`
	Slug          string        `json:"slug"`
	SellerID      uuid.UUID     `json:"seller_id"`
	Title         string        `json:"title"`
	Price         int64         `json:"price"`
	AllowOffers   bool          `json:"allow_offers"`
	Status        domain.Status `json:"status"`
	CoverThumbURL string        `json:"cover_thumb_url"`
	ReservedFor   uuid.UUID     `json:"reserved_for"`
	// CategoryCode and Condition let a similar-listings search boost the
	// source's kind.
	CategoryCode string           `json:"category_code"`
	Condition    domain.Condition `json:"condition"`
	// CommuneCode is the listing's commune; Places names it.
	CommuneCode string `json:"commune_code"`
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
// Copied is true once processed for a photo copied from the seller's
// listing elsewhere.
type PhotoView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url,omitempty"`
	ThumbURL string    `json:"thumb_url,omitempty"`
	Copied   bool      `json:"copied"`
	Failure  string    `json:"failure,omitempty"`
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
	now := c.now()
	out := make([]ListingView, 0, len(ls))
	for i := range ls {
		l := &ls[i]
		photos := make([]PhotoView, 0, len(l.Photos()))
		for _, p := range l.Photos() {
			v := PhotoView{UploadID: p.UploadID, Copied: p.Keys.Copied, Failure: l.Rejection()[p.UploadID]}
			if p.Keys.Photo != "" {
				v.URL, v.ThumbURL = c.media.URL(p.Keys.Photo), c.media.URL(p.Keys.Thumb)
			}
			photos = append(photos, v)
		}
		out = append(out, ListingView{
			ID: l.ID(), Slug: l.Slug(), SellerID: l.SellerID(), Title: l.Title(), Price: l.Price(), Description: l.Description(),
			CategoryCode: l.CategoryCode(), Condition: l.Condition(), AllowOffers: l.AllowOffers(),
			Location: places[l.CommuneCode()],
			Photos:   photos, Specs: l.Specs(), Status: l.Status(),
			CreatedAt: l.CreatedAt(), UpdatedAt: l.UpdatedAt(), PublishedAt: l.PublishedAt(), SoldAt: l.SoldAt(),
		})
		if d, ok := l.PriceDrop(now); ok {
			out[len(out)-1].PriceDrop = &d
		}
	}
	return out, nil
}

// own loads the seller's listing id. Another seller's listing is
// domain.ErrNotFound, so a listing's existence does not leak; a deleted one
// is the seller's still, and the domain refuses to change it.
func (c *Catalog) own(ctx context.Context, seller, id uuid.UUID) (domain.Listing, error) {
	l, err := c.listing(ctx, id)
	if err != nil {
		return domain.Listing{}, err
	}
	if l.SellerID() != seller {
		return domain.Listing{}, domain.ErrNotFound
	}
	return l, nil
}

// MarketPrice is a listing's seller and price beside the median price of the
// listings like it and how many there were.
type MarketPrice struct {
	SellerID   uuid.UUID
	Price      int64
	Median     int64
	SampleSize int
}

// DraftView is the ListingDraft schema in api/schemas/catalog.yaml.
type DraftView struct {
	ID            uuid.UUID        `json:"id"`
	SchemaVersion int              `json:"schema_version"`
	Title         string           `json:"title"`
	Price         *int64           `json:"price"`
	Photos        []DraftPhotoView `json:"photos"`
	Body          string           `json:"body"`
	UpdatedAt     time.Time        `json:"updated_at"`
	ExpiresAt     time.Time        `json:"expires_at"`
}

// DraftPhotoView is the ListingDraftPhoto schema: the URL reads the
// original upload for an hour.
type DraftPhotoView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url"`
}

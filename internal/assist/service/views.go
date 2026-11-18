package service

import (
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
)

// ListingReadView is the ListingRead schema in api/schemas/assist.yaml. A
// field a step has not reached yet is absent, and so is a field of another
// source.
type ListingReadView struct {
	ID      uuid.UUID       `json:"id"`
	Source  domain.Source   `json:"source"`
	Status  domain.Status   `json:"status"`
	Failure domain.Failure  `json:"failure,omitempty"`
	Link    *LinkSourceView `json:"link,omitempty"`
	Photos  []PhotoView     `json:"photos,omitempty"`
	Draft   *domain.Draft   `json:"draft,omitempty"`
	// ContactRemoved is set with a link read's draft.
	ContactRemoved *bool `json:"contact_removed,omitempty"`
	// Estimate is set once the read is done: a nil outer pointer leaves the
	// field out, a nil inner one sends null, for a read with no range.
	Estimate  **domain.Estimate `json:"estimate,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// PhotoView is a photo of the read with a URL the app reads it from for an
// hour; AtSeconds only for a video's frames.
type PhotoView struct {
	UploadID  uuid.UUID `json:"upload_id"`
	URL       string    `json:"url"`
	AtSeconds *float64  `json:"at_seconds,omitempty"`
}

// LinkSourceView is the listing the read copies from; PhotoCount is set
// after the read step.
type LinkSourceView struct {
	Site       string `json:"site"`
	URL        string `json:"url"`
	PhotoCount int    `json:"photo_count,omitempty"`
}

// LinkPreviewView is the LinkPreview schema: the card before a read.
type LinkPreviewView struct {
	Site       string `json:"site"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Price      *int64 `json:"price"`
	CoverURL   string `json:"cover_url"`
	PhotoCount int    `json:"photo_count"`
}

// PriceGuideView is the PriceGuide schema.
type PriceGuideView struct {
	Estimate    *domain.Estimate       `json:"estimate"`
	Suggestions []domain.Suggestion    `json:"suggestions"`
	Similar     []SimilarView          `json:"similar"`
	SoldPrices  []int64                `json:"sold_prices"`
	LivePrices  []int64                `json:"live_prices"`
	Watchers    []discoverysvc.Watcher `json:"watchers"`
}

// SimilarView is the SimilarListing schema; DaysToSell only when sold.
type SimilarView struct {
	ListingID  uuid.UUID               `json:"listing_id"`
	Title      string                  `json:"title"`
	CoverURL   string                  `json:"cover_url"`
	Price      int64                   `json:"price"`
	Condition  catalogdomain.Condition `json:"condition"`
	Sold       bool                    `json:"sold"`
	Km         int                     `json:"km"`
	DaysToSell *int                    `json:"days_to_sell,omitempty"`
}

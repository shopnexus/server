package service

import (
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// VideoReadView is the VideoRead schema in api/schemas/assist.yaml. A field
// a step has not reached yet is absent.
type VideoReadView struct {
	ID      uuid.UUID      `json:"id"`
	Status  domain.Status  `json:"status"`
	Failure domain.Failure `json:"failure,omitempty"`
	Frames  []FrameView    `json:"frames,omitempty"`
	Draft   *domain.Draft  `json:"draft,omitempty"`
	// Estimate is set once the read is done: a nil outer pointer leaves the
	// field out, a nil inner one sends null, for a read with no range.
	Estimate  **domain.Estimate `json:"estimate,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// FrameView is a frame with a URL the app reads it from for an hour.
type FrameView struct {
	UploadID  uuid.UUID `json:"upload_id"`
	URL       string    `json:"url"`
	AtSeconds float64   `json:"at_seconds"`
}

// LinkReadView is the LinkRead schema in api/schemas/assist.yaml. A field
// a step has not reached yet is absent.
type LinkReadView struct {
	ID      uuid.UUID      `json:"id"`
	Status  domain.Status  `json:"status"`
	Failure domain.Failure `json:"failure,omitempty"`
	Source  LinkSourceView `json:"source"`
	Photos  []CopyView     `json:"photos,omitempty"`
	Draft   *domain.Draft  `json:"draft,omitempty"`
	// ContactRemoved is set with the draft.
	ContactRemoved *bool `json:"contact_removed,omitempty"`
	// Estimate is set once the read is done: a nil outer pointer leaves the
	// field out, a nil inner one sends null.
	Estimate  **domain.Estimate `json:"estimate,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// LinkSourceView is the listing the read copies from; PhotoCount is set
// after the read step.
type LinkSourceView struct {
	Site       string `json:"site"`
	URL        string `json:"url"`
	PhotoCount int    `json:"photo_count,omitempty"`
}

// CopyView is a copied photo with a URL the app reads it from for an hour.
type CopyView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url"`
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

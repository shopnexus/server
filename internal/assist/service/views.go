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

// Package domain holds module assist's rules: a read of a seller's video
// or photo from its start to a draft listing, how the model's answer is
// cleaned, and the price range.
package domain

import (
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

const (
	// ReadWindow is the span each source's cap counts reads in.
	ReadWindow = 24 * time.Hour
	// KeepFor is how long a read is kept after it starts; then it is
	// deleted.
	KeepFor = 24 * time.Hour
	// Deadline is how long a read may run before it fails as unavailable.
	Deadline = 2 * time.Minute
	// MaxKept is how many frames a draft takes as photos.
	MaxKept = 6
)

type Status string

const (
	StatusReading Status = "reading"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Failure says why a read failed; ListingRead.Fail holds each source to its own.
type Failure string

const (
	FailUnreadable  Failure = "unreadable"
	FailTooShort    Failure = "video_too_short"
	FailTooLong     Failure = "video_too_long"
	FailNoItem      Failure = "no_item"
	FailUnavailable Failure = "unavailable"
)

// Draft is the listing the read filled, in ListingInput's field names. A
// field with a value is one the model filled.
type Draft struct {
	Title        string                  `json:"title"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	Specs        []catalogdomain.Spec    `json:"specs"`
	Description  string                  `json:"description"`
	// Price is the Chợ Tốt listing's price in an ad fill; nil in a read.
	Price       *int64      `json:"price"`
	AllowOffers bool        `json:"allow_offers"`
	Photos      []uuid.UUID `json:"photos"`
}

type EstimateSource string

const SourceMarket EstimateSource = "market"

// Estimate is a reference price range in whole đồng.
type Estimate struct {
	Low        int64          `json:"low"`
	Median     int64          `json:"median"`
	High       int64          `json:"high"`
	Source     EstimateSource `json:"source"`
	SampleSize int            `json:"sample_size"`
}

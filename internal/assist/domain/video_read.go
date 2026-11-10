// Package domain holds module assist's rules: a video read from its start
// to a draft listing, how the model's answer is cleaned, and the price
// range.
package domain

import (
	"encoding/json"
	"slices"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

const (
	// ReadLimit is how many reads an account may start in ReadWindow.
	ReadLimit  = 30
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

// Failure says why a read failed.
type Failure string

const (
	FailUnreadable  Failure = "unreadable_video"
	FailTooShort    Failure = "video_too_short"
	FailTooLong     Failure = "video_too_long"
	FailNoItem      Failure = "no_item"
	FailUnavailable Failure = "unavailable"
)

// Frame is a frame of the video, saved as a photo upload of the seller's.
type Frame struct {
	UploadID  uuid.UUID `json:"upload_id"`
	AtSeconds float64   `json:"at_seconds"`
}

// Draft is the listing the read filled, in ListingDraft's field names. A
// field with a value is one the model filled.
type Draft struct {
	Title        string                  `json:"title"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	Specs        []catalogdomain.Spec    `json:"specs"`
	Description  string                  `json:"description"`
	// Price is the price the seller said, nil when they said none.
	Price       *int64      `json:"price"`
	AllowOffers bool        `json:"allow_offers"`
	Photos      []uuid.UUID `json:"photos"`
}

type EstimateSource string

const (
	SourceMarket EstimateSource = "market"
	SourceModel  EstimateSource = "model"
)

// Estimate is a reference price range in whole đồng.
type Estimate struct {
	Low        int64          `json:"low"`
	Median     int64          `json:"median"`
	High       int64          `json:"high"`
	Source     EstimateSource `json:"source"`
	SampleSize int            `json:"sample_size"`
}

// VideoRead is one read of a seller's video. It moves from reading to done
// or failed; its frames, draft and estimate fill in as the steps finish.
type VideoRead struct {
	id, accountID, videoID uuid.UUID
	status                 Status
	failure                Failure
	frames                 []Frame
	draft                  *Draft
	estimate               *Estimate
	createdAt, expiresAt   time.Time
	writeID                uuid.UUID
}

// NewVideoRead starts a read of video for account. Its first write ID is
// its ID, so a retried insert writes the same row.
func NewVideoRead(id, account, video uuid.UUID, now time.Time) VideoRead {
	return VideoRead{
		id: id, accountID: account, videoID: video, status: StatusReading,
		createdAt: now, expiresAt: now.Add(KeepFor), writeID: id,
	}
}

func (r *VideoRead) ID() uuid.UUID        { return r.id }
func (r *VideoRead) AccountID() uuid.UUID { return r.accountID }
func (r *VideoRead) VideoID() uuid.UUID   { return r.videoID }
func (r *VideoRead) Status() Status       { return r.status }
func (r *VideoRead) Failure() Failure     { return r.failure }
func (r *VideoRead) Frames() []Frame      { return slices.Clone(r.frames) }
func (r *VideoRead) CreatedAt() time.Time { return r.createdAt }
func (r *VideoRead) ExpiresAt() time.Time { return r.expiresAt }
func (r *VideoRead) WriteID() uuid.UUID   { return r.writeID }

// Draft is a copy of the draft, nil before the fill step.
func (r *VideoRead) Draft() *Draft {
	if r.draft == nil {
		return nil
	}
	d := *r.draft
	return &d
}

// Estimate is a copy of the range, nil before the estimate step and when
// there is none.
func (r *VideoRead) Estimate() *Estimate {
	if r.estimate == nil {
		return nil
	}
	e := *r.estimate
	return &e
}

func (r *VideoRead) SetFrames(frames []Frame) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.frames = slices.Clone(frames)
	return nil
}

func (r *VideoRead) SetDraft(d Draft) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	d.Specs, d.Photos = slices.Clone(d.Specs), slices.Clone(d.Photos)
	r.draft = &d
	return nil
}

// Finish ends the read with its range, nil when there is none.
func (r *VideoRead) Finish(e *Estimate) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.status = StatusDone
	if e != nil {
		c := *e
		r.estimate = &c
	}
	return nil
}

func (r *VideoRead) Fail(f Failure) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.status, r.failure = StatusFailed, f
	return nil
}

// Expired reports a read past its 24 hours, which nobody reads any more.
func (r *VideoRead) Expired(now time.Time) bool { return !now.Before(r.expiresAt) }

// PastDeadline reports a read that has run for Deadline or longer.
func (r *VideoRead) PastDeadline(now time.Time) bool { return now.Sub(r.createdAt) >= Deadline }

// Saved records that a store wrote r under writeID. Stores only.
func (r *VideoRead) Saved(writeID uuid.UUID) { r.writeID = writeID }

// VideoReadState is every stored field of a read. Stores only.
type VideoReadState struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	VideoID   uuid.UUID `json:"video_id"`
	Status    Status    `json:"status"`
	Failure   Failure   `json:"failure"`
	Frames    []Frame   `json:"frames"`
	Draft     *Draft    `json:"draft"`
	Estimate  *Estimate `json:"estimate"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	WriteID   uuid.UUID `json:"write_id"`
}

// RestoreVideoRead rebuilds a stored read. Stores only.
func RestoreVideoRead(st VideoReadState) VideoRead {
	return VideoRead{
		id: st.ID, accountID: st.AccountID, videoID: st.VideoID, status: st.Status, failure: st.Failure,
		frames: st.Frames, draft: st.Draft, estimate: st.Estimate,
		createdAt: st.CreatedAt, expiresAt: st.ExpiresAt, writeID: st.WriteID,
	}
}

// State returns every stored field. Stores only.
func (r *VideoRead) State() VideoReadState {
	return VideoReadState{
		ID: r.id, AccountID: r.accountID, VideoID: r.videoID, Status: r.status, Failure: r.failure,
		Frames: r.Frames(), Draft: r.Draft(), Estimate: r.Estimate(),
		CreatedAt: r.createdAt, ExpiresAt: r.expiresAt, WriteID: r.writeID,
	}
}

// MarshalJSON lets a Restate step return a read for its journal.
func (r VideoRead) MarshalJSON() ([]byte, error) { return json.Marshal(r.State()) }

func (r *VideoRead) UnmarshalJSON(b []byte) error {
	var st VideoReadState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*r = RestoreVideoRead(st)
	return nil
}

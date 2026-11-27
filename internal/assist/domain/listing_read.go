package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"uuid"
)

// Source is what a read reads into a draft: the seller's video or one
// photo of theirs.
type Source string

const (
	SourceVideo Source = "video"
	SourcePhoto Source = "photo"
)

// The caps: how many reads of each source an account may start in
// ReadWindow. A photo read costs one image and no speech to text.
const (
	VideoCap = 30
	PhotoCap = 60
)

const (
	// MaxTranscript is the longest transcript a phone may send with its video.
	MaxTranscript = 1000
	// TranscriptWait is how long a transcript waits for its read's insert.
	TranscriptWait = 10 * time.Minute
)

// Cap is how many reads of s an account may start in ReadWindow.
func (s Source) Cap() int {
	switch s {
	case SourceVideo:
		return VideoCap
	case SourcePhoto:
		return PhotoCap
	}
	return 0
}

// failures are the ways each source's read can fail.
var failures = map[Source][]Failure{
	SourceVideo: {FailUnreadable, FailTooShort, FailTooLong, FailNoItem, FailUnavailable},
	SourcePhoto: {FailUnreadable, FailNoItem, FailUnavailable},
}

// Photo is a photo upload a read holds for the draft: a frame of the
// video with its time, or the photo read.
type Photo struct {
	UploadID  uuid.UUID `json:"upload_id"`
	AtSeconds *float64  `json:"at_seconds,omitempty"`
}

// ListingRead is one read of a seller's video or photo, named by its
// upload (media). It moves from reading to done or failed; its photos and
// draft fill in as the steps finish.
type ListingRead struct {
	id, accountID        uuid.UUID
	source               Source
	media                uuid.UUID
	status               Status
	failure              Failure
	reason               string
	photos               []Photo
	draft                *Draft
	createdAt, expiresAt time.Time
	writeID              uuid.UUID
}

// NewVideoRead starts a read of video for account. Its first write ID is
// its ID, so a retried insert writes the same row; the same goes for the
// other sources.
func NewVideoRead(id, account, video uuid.UUID, now time.Time) ListingRead {
	return ListingRead{
		id: id, accountID: account, source: SourceVideo, media: video, status: StatusReading,
		createdAt: now, expiresAt: now.Add(KeepFor), writeID: id,
	}
}

// NewPhotoRead starts a read of one photo upload of account's.
func NewPhotoRead(id, account, photo uuid.UUID, now time.Time) ListingRead {
	return ListingRead{
		id: id, accountID: account, source: SourcePhoto, media: photo, status: StatusReading,
		createdAt: now, expiresAt: now.Add(KeepFor), writeID: id,
	}
}

func (r *ListingRead) ID() uuid.UUID        { return r.id }
func (r *ListingRead) AccountID() uuid.UUID { return r.accountID }
func (r *ListingRead) Source() Source       { return r.source }

// Media is the video or photo upload read.
func (r *ListingRead) Media() uuid.UUID { return r.media }
func (r *ListingRead) Status() Status   { return r.status }
func (r *ListingRead) Failure() Failure { return r.failure }

// FailureReason says in a sentence why the read failed: the model's
// reason, or what went wrong asking it; empty for the other failures.
func (r *ListingRead) FailureReason() string { return r.reason }
func (r *ListingRead) Photos() []Photo       { return slices.Clone(r.photos) }
func (r *ListingRead) CreatedAt() time.Time  { return r.createdAt }
func (r *ListingRead) ExpiresAt() time.Time  { return r.expiresAt }
func (r *ListingRead) WriteID() uuid.UUID    { return r.writeID }

// Draft is a copy of the draft, nil before the step that fills it.
func (r *ListingRead) Draft() *Draft {
	if r.draft == nil {
		return nil
	}
	d := *r.draft
	return &d
}

func (r *ListingRead) SetPhotos(photos []Photo) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.photos = slices.Clone(photos)
	return nil
}

// SetDraft records the draft.
func (r *ListingRead) SetDraft(d Draft) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	d.Specs, d.Photos = slices.Clone(d.Specs), slices.Clone(d.Photos)
	r.draft = &d
	return nil
}

// Finish ends the read; the seller prices the draft.
func (r *ListingRead) Finish() error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.status = StatusDone
	return nil
}

// Fail ends the read with f, one of the failures its source can have, and
// the reason for the seller, cut to MaxReason.
func (r *ListingRead) Fail(f Failure, reason string) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	if !slices.Contains(failures[r.source], f) {
		return ErrWrongFailure
	}
	r.status, r.failure, r.reason = StatusFailed, f, cut(strings.TrimSpace(reason), MaxReason)
	return nil
}

// Expired reports a read past its 24 hours, which nobody reads any more.
func (r *ListingRead) Expired(now time.Time) bool { return !now.Before(r.expiresAt) }

// PastDeadline reports a read that has run for Deadline or longer.
func (r *ListingRead) PastDeadline(now time.Time) bool { return now.Sub(r.createdAt) >= Deadline }

// Saved records that a store wrote r under writeID. Stores only.
func (r *ListingRead) Saved(writeID uuid.UUID) { r.writeID = writeID }

// ListingReadState is every stored field of a read. Stores only. It has no
// transcript: a read is journaled, and the transcript never is.
type ListingReadState struct {
	ID            uuid.UUID `json:"id"`
	AccountID     uuid.UUID `json:"account_id"`
	Source        Source    `json:"source"`
	Media         uuid.UUID `json:"media"`
	Status        Status    `json:"status"`
	Failure       Failure   `json:"failure"`
	FailureReason string    `json:"failure_reason"`
	Photos        []Photo   `json:"photos"`
	Draft         *Draft    `json:"draft"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	WriteID       uuid.UUID `json:"write_id"`
}

// RestoreListingRead rebuilds a stored read. Stores only.
func RestoreListingRead(st ListingReadState) ListingRead {
	return ListingRead{
		id: st.ID, accountID: st.AccountID, source: st.Source, media: st.Media,
		status: st.Status, failure: st.Failure, reason: st.FailureReason, photos: st.Photos, draft: st.Draft,
		createdAt: st.CreatedAt, expiresAt: st.ExpiresAt, writeID: st.WriteID,
	}
}

// State returns every stored field. Stores only.
func (r *ListingRead) State() ListingReadState {
	return ListingReadState{
		ID: r.id, AccountID: r.accountID, Source: r.source, Media: r.media,
		Status: r.status, Failure: r.failure, FailureReason: r.reason, Photos: r.Photos(), Draft: r.Draft(),
		CreatedAt: r.createdAt, ExpiresAt: r.expiresAt, WriteID: r.writeID,
	}
}

// MarshalJSON lets a Restate step return a read for its journal.
func (r ListingRead) MarshalJSON() ([]byte, error) { return json.Marshal(r.State()) }

func (r *ListingRead) UnmarshalJSON(b []byte) error {
	var st ListingReadState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*r = RestoreListingRead(st)
	return nil
}

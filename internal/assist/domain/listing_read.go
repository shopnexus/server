package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"uuid"
)

// Source is what a read reads into a draft: the seller's video, one photo
// of theirs, or their listing on another site.
type Source string

const (
	SourceVideo Source = "video"
	SourcePhoto Source = "photo"
	SourceLink  Source = "link"
)

// The caps: how many reads of each source an account may start in
// ReadWindow. A photo read costs one image and no speech to text.
const (
	VideoCap = 30
	PhotoCap = 60
	LinkCap  = 30
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
	case SourceLink:
		return LinkCap
	}
	return 0
}

// failures are the ways each source's read can fail.
var failures = map[Source][]Failure{
	SourceVideo: {FailUnreadable, FailTooShort, FailTooLong, FailNoItem, FailUnavailable},
	SourcePhoto: {FailUnreadable, FailNoItem, FailUnavailable},
	SourceLink:  {FailListingNotFound, FailNoItem, FailUnavailable},
}

// Photo is a photo upload a read holds for the draft: a frame of the
// video with its time, a copy of the listing's photo, or the photo read.
type Photo struct {
	UploadID  uuid.UUID `json:"upload_id"`
	AtSeconds *float64  `json:"at_seconds,omitempty"`
}

// ListingRead is one read of a seller's video, photo or listing elsewhere.
// It moves from reading to done or failed; its photos, draft and estimate
// fill in as the steps finish. A video or photo read names its upload
// (media), a link read its link and the photos the old listing has.
type ListingRead struct {
	id, accountID        uuid.UUID
	source               Source
	media                uuid.UUID
	link                 Link
	linkPhotos           int
	status               Status
	failure              Failure
	reason               string
	photos               []Photo
	draft                *Draft
	contactRemoved       bool
	estimate             *Estimate
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

// NewLinkRead starts a read of account's listing at link.
func NewLinkRead(id, account uuid.UUID, link Link, now time.Time) ListingRead {
	return ListingRead{
		id: id, accountID: account, source: SourceLink, link: link, status: StatusReading,
		createdAt: now, expiresAt: now.Add(KeepFor), writeID: id,
	}
}

func (r *ListingRead) ID() uuid.UUID        { return r.id }
func (r *ListingRead) AccountID() uuid.UUID { return r.accountID }
func (r *ListingRead) Source() Source       { return r.source }

// Media is the video or photo upload read; zero for a link read.
func (r *ListingRead) Media() uuid.UUID { return r.media }

// Link is the listing a link read reads; zero for the others.
func (r *ListingRead) Link() Link { return r.link }

// LinkPhotos is how many photos the old listing has, 0 before the read step.
func (r *ListingRead) LinkPhotos() int  { return r.linkPhotos }
func (r *ListingRead) Status() Status   { return r.status }
func (r *ListingRead) Failure() Failure { return r.failure }

// FailureReason says in a sentence why the read failed: the model's
// reason, or what went wrong asking it; empty for the other failures.
func (r *ListingRead) FailureReason() string { return r.reason }
func (r *ListingRead) Photos() []Photo       { return slices.Clone(r.photos) }
func (r *ListingRead) ContactRemoved() bool  { return r.contactRemoved }
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

// Estimate is a copy of the range, nil before the end and when there is none.
func (r *ListingRead) Estimate() *Estimate {
	if r.estimate == nil {
		return nil
	}
	e := *r.estimate
	return &e
}

func (r *ListingRead) SetPhotos(photos []Photo) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.photos = slices.Clone(photos)
	return nil
}

// SetLinkPhotos records how many photos a link's listing has.
func (r *ListingRead) SetLinkPhotos(n int) error {
	if r.source != SourceLink {
		return ErrWrongSource
	}
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.linkPhotos = n
	return nil
}

// SetDraft records the draft and, for a link read, whether a contact was
// taken out of its description.
func (r *ListingRead) SetDraft(d Draft, contactRemoved bool) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	d.Specs, d.Photos = slices.Clone(d.Specs), slices.Clone(d.Photos)
	r.draft, r.contactRemoved = &d, contactRemoved && r.source == SourceLink
	return nil
}

// Finish ends the read with its range, nil when there is none.
func (r *ListingRead) Finish(e *Estimate) error {
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
	ID             uuid.UUID `json:"id"`
	AccountID      uuid.UUID `json:"account_id"`
	Source         Source    `json:"source"`
	Media          uuid.UUID `json:"media"`
	Link           Link      `json:"link"`
	LinkPhotos     int       `json:"link_photos"`
	Status         Status    `json:"status"`
	Failure        Failure   `json:"failure"`
	FailureReason  string    `json:"failure_reason"`
	Photos         []Photo   `json:"photos"`
	Draft          *Draft    `json:"draft"`
	ContactRemoved bool      `json:"contact_removed"`
	Estimate       *Estimate `json:"estimate"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	WriteID        uuid.UUID `json:"write_id"`
}

// RestoreListingRead rebuilds a stored read. Stores only.
func RestoreListingRead(st ListingReadState) ListingRead {
	return ListingRead{
		id: st.ID, accountID: st.AccountID, source: st.Source, media: st.Media, link: st.Link, linkPhotos: st.LinkPhotos,
		status: st.Status, failure: st.Failure, reason: st.FailureReason, photos: st.Photos, draft: st.Draft, contactRemoved: st.ContactRemoved,
		estimate: st.Estimate, createdAt: st.CreatedAt, expiresAt: st.ExpiresAt, writeID: st.WriteID,
	}
}

// State returns every stored field. Stores only.
func (r *ListingRead) State() ListingReadState {
	return ListingReadState{
		ID: r.id, AccountID: r.accountID, Source: r.source, Media: r.media, Link: r.link, LinkPhotos: r.linkPhotos,
		Status: r.status, Failure: r.failure, FailureReason: r.reason, Photos: r.Photos(), Draft: r.Draft(), ContactRemoved: r.contactRemoved,
		Estimate: r.Estimate(), CreatedAt: r.createdAt, ExpiresAt: r.expiresAt, WriteID: r.writeID,
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

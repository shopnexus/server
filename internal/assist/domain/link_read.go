package domain

import (
	"encoding/json"
	"slices"
	"time"
	"uuid"
)

// LinkRead is one read of a seller's listing on another site. It moves
// from reading to done or failed; the photo count, the draft (with the
// copied photos) and the estimate fill in as the steps finish.
type LinkRead struct {
	id, accountID        uuid.UUID
	link                 Link
	status               Status
	failure              Failure
	photoCount           int
	draft                *Draft
	contactRemoved       bool
	estimate             *Estimate
	createdAt, expiresAt time.Time
	writeID              uuid.UUID
}

// NewLinkRead starts a read of link for account. Its first write ID is its
// ID, so a retried insert writes the same row.
func NewLinkRead(id, account uuid.UUID, link Link, now time.Time) LinkRead {
	return LinkRead{
		id: id, accountID: account, link: link, status: StatusReading,
		createdAt: now, expiresAt: now.Add(KeepFor), writeID: id,
	}
}

func (r *LinkRead) ID() uuid.UUID        { return r.id }
func (r *LinkRead) AccountID() uuid.UUID { return r.accountID }
func (r *LinkRead) Link() Link           { return r.link }
func (r *LinkRead) Status() Status       { return r.status }
func (r *LinkRead) Failure() Failure     { return r.failure }

// PhotoCount is how many photos the old listing has, 0 before the read step.
func (r *LinkRead) PhotoCount() int      { return r.photoCount }
func (r *LinkRead) ContactRemoved() bool { return r.contactRemoved }
func (r *LinkRead) CreatedAt() time.Time { return r.createdAt }
func (r *LinkRead) ExpiresAt() time.Time { return r.expiresAt }
func (r *LinkRead) WriteID() uuid.UUID   { return r.writeID }

// Draft is a copy of the draft, nil before the copy step.
func (r *LinkRead) Draft() *Draft {
	if r.draft == nil {
		return nil
	}
	d := *r.draft
	return &d
}

// Estimate is a copy of the range, nil before the end and when there is none.
func (r *LinkRead) Estimate() *Estimate {
	if r.estimate == nil {
		return nil
	}
	e := *r.estimate
	return &e
}

func (r *LinkRead) SetPhotoCount(n int) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.photoCount = n
	return nil
}

// SetDraft records the draft, its photos the copies, and whether a contact
// was taken out of its description.
func (r *LinkRead) SetDraft(d Draft, contactRemoved bool) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	d.Specs, d.Photos = slices.Clone(d.Specs), slices.Clone(d.Photos)
	r.draft, r.contactRemoved = &d, contactRemoved
	return nil
}

// Finish ends the read with its range, nil when there is none.
func (r *LinkRead) Finish(e *Estimate) error {
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

func (r *LinkRead) Fail(f Failure) error {
	if r.status != StatusReading {
		return ErrNotReading
	}
	r.status, r.failure = StatusFailed, f
	return nil
}

// Expired reports a read past its 24 hours, which nobody reads any more.
func (r *LinkRead) Expired(now time.Time) bool { return !now.Before(r.expiresAt) }

// Saved records that a store wrote r under writeID. Stores only.
func (r *LinkRead) Saved(writeID uuid.UUID) { r.writeID = writeID }

// LinkReadState is every stored field of a link read. Stores only.
type LinkReadState struct {
	ID             uuid.UUID `json:"id"`
	AccountID      uuid.UUID `json:"account_id"`
	Link           Link      `json:"link"`
	Status         Status    `json:"status"`
	Failure        Failure   `json:"failure"`
	PhotoCount     int       `json:"photo_count"`
	Draft          *Draft    `json:"draft"`
	ContactRemoved bool      `json:"contact_removed"`
	Estimate       *Estimate `json:"estimate"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	WriteID        uuid.UUID `json:"write_id"`
}

// RestoreLinkRead rebuilds a stored read. Stores only.
func RestoreLinkRead(st LinkReadState) LinkRead {
	return LinkRead{
		id: st.ID, accountID: st.AccountID, link: st.Link, status: st.Status, failure: st.Failure,
		photoCount: st.PhotoCount, draft: st.Draft, contactRemoved: st.ContactRemoved, estimate: st.Estimate,
		createdAt: st.CreatedAt, expiresAt: st.ExpiresAt, writeID: st.WriteID,
	}
}

// State returns every stored field. Stores only.
func (r *LinkRead) State() LinkReadState {
	return LinkReadState{
		ID: r.id, AccountID: r.accountID, Link: r.link, Status: r.status, Failure: r.failure,
		PhotoCount: r.photoCount, Draft: r.Draft(), ContactRemoved: r.contactRemoved, Estimate: r.Estimate(),
		CreatedAt: r.createdAt, ExpiresAt: r.expiresAt, WriteID: r.writeID,
	}
}

// MarshalJSON lets a Restate step return a read for its journal.
func (r LinkRead) MarshalJSON() ([]byte, error) { return json.Marshal(r.State()) }

func (r *LinkRead) UnmarshalJSON(b []byte) error {
	var st LinkReadState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*r = RestoreLinkRead(st)
	return nil
}

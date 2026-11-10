package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

// Reason is why a listing or an account is reported. Condition and price
// are for buyer and seller to settle, so no reason names them.
type Reason string

const (
	ReasonBanned Reason = "banned"
	ReasonFake   Reason = "fake"
	ReasonScam   Reason = "scam"
	ReasonOther  Reason = "other"
)

var reasons = []Reason{ReasonBanned, ReasonFake, ReasonScam, ReasonOther}

// MaxNote caps a report's note, counted after trimming.
const MaxNote = 500

// Report is a person telling ShopNexus that a listing or an account breaks
// the rules. AccountID is the account reported, the seller for a listing;
// ListingID is zero for an account report.
type Report struct {
	id          uuid.UUID
	reporterID  uuid.UUID
	accountID   uuid.UUID
	listingID   uuid.UUID
	reason      Reason
	note        string
	createdAt   time.Time
	updatedAt   time.Time
	withdrawnAt *time.Time
}

// NewReport checks a report: not of the reporter, a known reason, and a
// note of at most 500 characters after trimming.
func NewReport(id, reporter, account, listing uuid.UUID, reason Reason, note string, now time.Time) (Report, error) {
	if reporter == account {
		return Report{}, ErrSelfReport
	}
	e := ErrInvalidReport
	note = strings.TrimSpace(note)
	if !slices.Contains(reasons, reason) {
		e = e.WithField("reason", "banned, fake, scam or other")
	}
	if utf8.RuneCountInString(note) > MaxNote {
		e = e.WithField("note", "at most 500 characters")
	}
	if len(e.Fields) > 0 {
		return Report{}, e
	}
	return Report{
		id: id, reporterID: reporter, accountID: account, listingID: listing, reason: reason, note: note,
		createdAt: now, updatedAt: now,
	}, nil
}

func (r *Report) ID() uuid.UUID           { return r.id }
func (r *Report) ReporterID() uuid.UUID   { return r.reporterID }
func (r *Report) AccountID() uuid.UUID    { return r.accountID }
func (r *Report) ListingID() uuid.UUID    { return r.listingID }
func (r *Report) Reason() Reason          { return r.reason }
func (r *Report) Note() string            { return r.note }
func (r *Report) CreatedAt() time.Time    { return r.createdAt }
func (r *Report) UpdatedAt() time.Time    { return r.updatedAt }
func (r *Report) WithdrawnAt() *time.Time { return r.withdrawnAt }

// ReportState is every stored field of a report. Stores only.
type ReportState struct {
	ID          uuid.UUID
	ReporterID  uuid.UUID
	AccountID   uuid.UUID
	ListingID   uuid.UUID
	Reason      Reason
	Note        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	WithdrawnAt *time.Time
}

// RestoreReport rebuilds a stored report. Stores only.
func RestoreReport(st ReportState) Report {
	return Report{
		id: st.ID, reporterID: st.ReporterID, accountID: st.AccountID, listingID: st.ListingID, reason: st.Reason,
		note: st.Note, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt, withdrawnAt: st.WithdrawnAt,
	}
}

// State returns every stored field. Stores only.
func (r *Report) State() ReportState {
	return ReportState{
		ID: r.id, ReporterID: r.reporterID, AccountID: r.accountID, ListingID: r.listingID, Reason: r.reason,
		Note: r.note, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt, WithdrawnAt: r.withdrawnAt,
	}
}

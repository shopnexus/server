package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
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
// ListingID is zero for an account report. ConversationID, on an account
// report only, is the reporter's conversation with the account; zero when
// none was named.
type Report struct {
	s ReportState
}

// ReportStatus is where a report stands for its reporter.
type ReportStatus string

const (
	ReportOpen      ReportStatus = "open"
	ReportWithdrawn ReportStatus = "withdrawn"
	ReportResolved  ReportStatus = "resolved"
)

// NewReport checks a report: not of the reporter, a known reason, a note of
// at most 500 characters after trimming, and a conversation only beside an
// account report.
func NewReport(id, reporter, account, listing, conversation uuid.UUID, reason Reason, note string, now time.Time) (Report, error) {
	if reporter == account {
		return Report{}, ErrSelfReport
	}
	e := ErrInvalidReport
	note = strings.TrimSpace(note)
	if !slices.Contains(reasons, reason) {
		e = e.WithField("reason", apperr.NotAllowed, "banned, fake, scam or other")
	}
	if utf8.RuneCountInString(note) > MaxNote {
		e = e.WithField("note", apperr.TooLong, "at most 500 characters")
	}
	if conversation != (uuid.UUID{}) && listing != (uuid.UUID{}) {
		e = e.WithField("conversation_id", apperr.NotAllowed, "only with account_id")
	}
	if len(e.Fields) > 0 {
		return Report{}, e
	}
	return Report{s: ReportState{
		ID: id, ReporterID: reporter, AccountID: account, ListingID: listing, ConversationID: conversation,
		Reason: reason, Note: note, CreatedAt: now, UpdatedAt: now,
	}}, nil
}

func (r *Report) ID() uuid.UUID             { return r.s.ID }
func (r *Report) ReporterID() uuid.UUID     { return r.s.ReporterID }
func (r *Report) AccountID() uuid.UUID      { return r.s.AccountID }
func (r *Report) ListingID() uuid.UUID      { return r.s.ListingID }
func (r *Report) ConversationID() uuid.UUID { return r.s.ConversationID }
func (r *Report) Reason() Reason            { return r.s.Reason }
func (r *Report) Note() string              { return r.s.Note }
func (r *Report) CreatedAt() time.Time      { return r.s.CreatedAt }
func (r *Report) UpdatedAt() time.Time      { return r.s.UpdatedAt }
func (r *Report) WithdrawnAt() *time.Time   { return r.s.WithdrawnAt }

// Status is withdrawn once the reporter withdrew the report, else resolved
// once its case was resolved, else open.
func (r *Report) Status() ReportStatus {
	switch {
	case r.s.WithdrawnAt != nil:
		return ReportWithdrawn
	case r.s.Resolved:
		return ReportResolved
	}
	return ReportOpen
}

// ReportState is every stored field of a report. Stores only.
type ReportState struct {
	ID             uuid.UUID
	ReporterID     uuid.UUID
	AccountID      uuid.UUID
	ListingID      uuid.UUID
	ConversationID uuid.UUID
	Reason         Reason
	Note           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	WithdrawnAt    *time.Time
	// Resolved is set once a console member resolved the report's case.
	Resolved bool
}

// RestoreReport rebuilds a stored report. Stores only.
func RestoreReport(st ReportState) Report { return Report{s: st} }

// State returns every stored field. Stores only.
func (r *Report) State() ReportState { return r.s }

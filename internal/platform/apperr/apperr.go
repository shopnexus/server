// Package apperr is the error type entities and use cases return. It carries
// a kind and a stable code and knows nothing about HTTP; httpx maps kinds to
// status codes.
package apperr

import (
	"errors"
	"maps"
	"math"
	"slices"
	"time"
)

// Kind classifies an error by what the caller can do about it.
type Kind int

const (
	KindInternal             Kind = iota // 500
	KindBadRequest                       // 400: the request is malformed
	KindInvalid                          // 422: well formed, but a business rule refuses it
	KindNotFound                         // 404
	KindConflict                         // 409
	KindUnauthenticated                  // 401
	KindForbidden                        // 403
	KindRateLimited                      // 429
	KindPreconditionRequired             // 428
	KindTooLarge                         // 413: the request body is over the limit
	KindUnavailable                      // 503: a service this request needs is down; try again
	KindTimeout                          // 504: a service this request needs did not answer in time
)

// Error is a business error. Code is stable and snake_case; Message is English
// for developers; Err is a cause that is logged and never sent to clients.
// Fields holds each bad field's problem in English, FieldCodes the same
// problem as a code. The JSON form lets a Restate service return it as
// data; Err stays out.
type Error struct {
	Kind       Kind                 `json:"kind"`
	Code       string               `json:"code"`
	Message    string               `json:"message"`
	Fields     map[string]string    `json:"fields,omitempty"`
	FieldCodes map[string]FieldCode `json:"field_codes,omitempty"`
	Details    Details              `json:"details,omitzero"`
	Err        error                `json:"-"`
}

// FieldCode says what is wrong with a field, so a client writes it in its
// own words. The list is Error.field_codes in api/schemas/error.yaml.
type FieldCode string

const (
	Required     FieldCode = "required"      // absent or empty
	TooShort     FieldCode = "too_short"     // text under its least length
	TooLong      FieldCode = "too_long"      // text over its most length
	TooFew       FieldCode = "too_few"       // a list under its least count
	TooMany      FieldCode = "too_many"      // a list over its most count
	OutOfRange   FieldCode = "out_of_range"  // a number or a time outside its bounds
	OffStep      FieldCode = "off_step"      // an amount between two steps
	Invalid      FieldCode = "invalid"       // the wrong type or format
	NotAllowed   FieldCode = "not_allowed"   // a value the field does not take here
	Unknown      FieldCode = "unknown"       // names nothing that exists
	Duplicate    FieldCode = "duplicate"     // a list holds an item twice
	OneOf        FieldCode = "one_of"        // takes exactly one of several fields
	MissingSpecs FieldCode = "missing_specs" // specs the category requires; details.missing_specs
	ParcelFailed FieldCode = "parcel_failed" // the carrier reports the parcel failed
	InUse        FieldCode = "in_use"        // something else still uses it
)

// Length is the code of a text of n characters outside min to max: empty,
// too short or too long; "" within.
func Length(n, minimum, maximum int) FieldCode {
	switch {
	case n == 0 && minimum > 0:
		return Required
	case n < minimum:
		return TooShort
	case n > maximum:
		return TooLong
	default:
		return ""
	}
}

// Count is Length for a list of n items.
func Count(n, minimum, maximum int) FieldCode {
	switch {
	case n == 0 && minimum > 0:
		return Required
	case n < minimum:
		return TooFew
	case n > maximum:
		return TooMany
	default:
		return ""
	}
}

// Details are the values a client acts on, the properties of Error.details
// in api/schemas/error.yaml. A detail that was set is sent, even 0 or "";
// a nil one is not, and an error with none sends no details.
type Details struct {
	RetryAfter   *int     `json:"retry_after,omitempty"`   // seconds
	AttemptsLeft *int     `json:"attempts_left,omitempty"` // guesses a code still takes
	Reason       *string  `json:"reason,omitempty"`        // a carrier's words
	MissingSpecs []string `json:"missing_specs,omitempty"` // spec labels a listing lacks
}

// ErrTryAgain answers a write that lost a race with another write to the
// same aggregate. Every module uses it, so a client handles one code.
var ErrTryAgain = New(KindConflict, "try_again", "the request collided with another; try again")

// From returns the *Error in err's chain. Any other error becomes a
// KindInternal Error, which the HTTP layer answers with a 500.
func From(err error) *Error {
	if e, ok := errors.AsType[*Error](err); ok {
		return e
	}
	return &Error{Kind: KindInternal, Code: "internal", Message: err.Error()}
}

// New returns an Error without fields or cause.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func (e *Error) Error() string {
	s := e.Code + ": " + e.Message
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// Is matches another *Error with the same code, so a copy made by WithField
// still matches the sentinel it came from.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// WithCause returns a copy of e whose cause is err.
func (e *Error) WithCause(err error) *Error {
	c := *e
	c.Err = err
	return &c
}

// WithKind returns a copy of e of another kind with the same code, for a
// refusal whose status depends on the operation that meets it.
func (e *Error) WithKind(kind Kind) *Error {
	c := *e
	c.Kind = kind
	return &c
}

// WithField returns a copy of e with one more field problem: its code and
// its English text. The copy has maps of its own, so the Error it came from
// keeps its fields.
func (e *Error) WithField(name string, code FieldCode, problem string) *Error {
	c := *e
	c.Fields, c.FieldCodes = maps.Clone(e.Fields), maps.Clone(e.FieldCodes)
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	if c.FieldCodes == nil {
		c.FieldCodes = map[string]FieldCode{}
	}
	c.Fields[name], c.FieldCodes[name] = problem, code
	return &c
}

// WithRetryAfter returns a copy of e whose retry_after detail is wait in
// whole seconds, rounded up, so a client that waits it is not refused again.
func (e *Error) WithRetryAfter(wait time.Duration) *Error {
	c := *e
	seconds := int(math.Ceil(wait.Seconds()))
	c.Details.RetryAfter = &seconds
	return &c
}

// WithAttemptsLeft returns a copy of e whose attempts_left detail is n.
func (e *Error) WithAttemptsLeft(n int) *Error {
	c := *e
	c.Details.AttemptsLeft = &n
	return &c
}

// WithMissingSpecs returns a copy of e whose missing_specs detail is labels.
func (e *Error) WithMissingSpecs(labels []string) *Error {
	c := *e
	c.Details.MissingSpecs = slices.Clone(labels)
	return &c
}

// WithReason returns a copy of e whose reason detail is reason.
func (e *Error) WithReason(reason string) *Error {
	c := *e
	c.Details.Reason = &reason
	return &c
}

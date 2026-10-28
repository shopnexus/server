// Package apperr is the error type entities and use cases return. It carries
// a kind and a stable code and knows nothing about HTTP; httpx maps kinds to
// status codes.
package apperr

import (
	"errors"
	"maps"
	"math"
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
)

// Error is a business error. Code is stable and snake_case; Message is English
// for developers; Err is a cause that is logged and never sent to clients.
// The JSON form lets a Restate service return it as data; Err stays out.
type Error struct {
	Kind    Kind              `json:"kind"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Details map[string]any    `json:"details,omitempty"` // values the client acts on: attempts_left, retry_after
	Err     error             `json:"-"`
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

// WithField returns a copy of e with one more field problem.
func (e *Error) WithField(name, problem string) *Error {
	c := *e
	c.Fields = maps.Clone(e.Fields)
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	c.Fields[name] = problem
	return &c
}

// WithRetryAfter returns a copy of e whose retry_after detail is wait in
// whole seconds, rounded up, so a client that waits it is not refused again.
func (e *Error) WithRetryAfter(wait time.Duration) *Error {
	return e.WithDetail("retry_after", int(math.Ceil(wait.Seconds())))
}

// WithDetail returns a copy of e with one more detail.
func (e *Error) WithDetail(name string, value any) *Error {
	c := *e
	c.Details = maps.Clone(e.Details)
	if c.Details == nil {
		c.Details = map[string]any{}
	}
	c.Details[name] = value
	return &c
}

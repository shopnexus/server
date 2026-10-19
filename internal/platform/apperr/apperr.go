// Package apperr is the error type entities and use cases return. It carries
// a kind and a stable code and knows nothing about HTTP; httpx maps kinds to
// status codes.
package apperr

import "maps"

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
)

// Error is a business error. Code is stable and snake_case; Message is English
// for developers; Err is a cause that is logged and never sent to clients.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  map[string]string
	Err     error
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

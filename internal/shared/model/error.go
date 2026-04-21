package sharedmodel

import (
	"fmt"
	"net/http"

	restate "github.com/restatedev/sdk-go"
)

// Error is the canonical API error envelope. Serialized to FE as
//
//	{"http_status": 409, "code": "wallet_not_empty", "message": "..."}
//
// The Code string is embedded as a message prefix ("code: message") so the
// identifier survives Restate cross-service wire serialization — the prefix
// is re-parsed by response.writeError when the struct is stripped on the wire.
// For generic fallbacks without a code, pass code="" to NewError.
type Error struct {
	HTTPStatus uint16 `json:"http_status"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e Error) Error() string      { return e.Message }
func (e Error) StatusCode() uint16 { return e.HTTPStatus }

// Fmt formats the message using fmt.Sprintf(e.Message, args...). Useful for
// package-level error vars that embed format specifiers in the message.
// The Code and HTTPStatus are preserved. The prefix is preserved as-is.
func (e Error) Fmt(args ...any) Error {
	return Error{
		HTTPStatus: e.HTTPStatus,
		Code:       e.Code,
		Message:    fmt.Sprintf(e.Message, args...),
	}
}

// Terminal wraps as a Restate terminal error. Required before returning from
// biz methods so Restate does not retry.
func (e Error) Terminal() error {
	return restate.TerminalError(e, restate.Code(e.HTTPStatus))
}

// NewError creates a structured Error with an optional snake_case code. The
// code is embedded as a "code: message" prefix so it survives Restate wire
// serialization (response.writeError re-extracts it when the struct is lost).
func NewError(status uint16, code string, message string) Error {
	msg := message
	if code != "" {
		msg = fmt.Sprintf("%s: %s", code, message)
	}
	return Error{HTTPStatus: status, Code: code, Message: msg}
}

// WrapErr wraps an error with context while preserving Restate terminal status and error code.
// Use this instead of fmt.Errorf when the error might be a terminal error.
func WrapErr(msg string, err error) error {
	if restate.IsTerminalError(err) {
		code := restate.ErrorCode(err)
		return restate.TerminalError(fmt.Errorf("%s: %w", msg, err), code)
	}
	return fmt.Errorf("%s: %w", msg, err)
}

var (
	ErrValidation     = NewError(http.StatusBadRequest, "validation", "%s")
	ErrEntityNotFound = NewError(http.StatusNotFound, "entity_not_found", "%s not found")
)

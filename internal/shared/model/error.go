package sharedmodel

import (
	"fmt"
	"net/http"

	restate "github.com/restatedev/sdk-go"
)

// Error is the canonical API error envelope. The JSON shape is:
//
//	{"http_status": 409, "code": "wallet_not_empty", "message": "..."}
//
// Code is an app-level string identifier (snake_case) that the FE branches on;
// HTTPStatus is the transport-level status used for HTTP and Restate wire codes.
//
// For cross-service safety, NewError embeds Code into Message as a prefix
// ("<code>: <message>"). When an error crosses a Restate service boundary the
// struct is stripped to (string, uint16), but the prefix survives in Message
// and response.writeError re-extracts it into Code — so FE sees the string
// code consistently whether the error is same-process or cross-service.
type Error struct {
	HTTPStatus uint16 `json:"http_status"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e Error) Error() string {
	return e.Message
}

func (e Error) StatusCode() uint16 {
	return e.HTTPStatus
}

func (e Error) Fmt(args ...any) Error {
	return Error{
		HTTPStatus: e.HTTPStatus,
		Code:       e.Code,
		Message:    fmt.Sprintf(e.Message, args...),
	}
}

// Terminal wraps as a Restate terminal error, preventing retries.
func (e Error) Terminal() error {
	return restate.TerminalError(e, restate.Code(e.HTTPStatus))
}

// NewError creates a structured error with a string identifier. The code
// is embedded as a message prefix ("code: message") so it survives cross-
// service Restate wire serialization; response.writeError re-parses the prefix
// back into the Code field on the way out to the FE. Pass code="" for generic
// fallbacks where no structured identifier applies.
func NewError(status uint16, code string, message string) Error {
	msg := message
	if code != "" {
		msg = fmt.Sprintf("%s: %s", code, message)
	}
	return Error{
		HTTPStatus: status,
		Code:       code,
		Message:    msg,
	}
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

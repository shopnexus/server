package sharedmodel

import (
	"fmt"
	"net/http"

	restate "github.com/restatedev/sdk-go"
)

type Error struct {
	ErrCode uint16 `json:"code"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	return e.Message
}

func (e Error) Code() uint16 {
	return e.ErrCode
}

// Fmt creates a new error from the base error template with provided arguments.
func (e Error) Fmt(args ...any) Error {
	return Error{
		ErrCode: e.ErrCode,
		Message: fmt.Sprintf(e.Message, args...),
	}
}

// Terminal wraps this error as a Restate terminal error, stopping retries.
func (e Error) Terminal() error {
	return restate.TerminalError(e, restate.Code(e.ErrCode))
}

func NewError(code uint16, message string) Error {
	return Error{
		ErrCode: code,
		Message: message,
	}
}

var (
	ErrValidation     = NewError(http.StatusBadRequest, "Validation error: %s")
	ErrEntityNotFound = NewError(http.StatusNotFound, "%s not found")
)

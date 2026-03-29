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

func (e Error) Fmt(args ...any) Error {
	return Error{
		ErrCode: e.ErrCode,
		Message: fmt.Sprintf(e.Message, args...),
	}
}

// Terminal wraps as a Restate terminal error, preventing retries.
func (e Error) Terminal() error {
	return restate.TerminalError(e, restate.Code(e.ErrCode))
}

func NewError(code uint16, message string) Error {
	return Error{
		ErrCode: code,
		Message: message,
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
	ErrValidation     = NewError(http.StatusBadRequest, "validation: %s")
	ErrEntityNotFound = NewError(http.StatusNotFound, "%s not found")
)

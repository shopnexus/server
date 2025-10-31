package sharedmodel

import "fmt"

type ErrorWithCode interface {
	Error() string
	Code() string
}

type Error struct {
	ErrCode string `json:"code"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	return e.Message
}

func (e Error) Code() string {
	return e.ErrCode
}

// Fmt creates a new error from the base error template with provided arguments
func (e Error) Fmt(args ...interface{}) Error {
	return Error{
		ErrCode: e.ErrCode,
		Message: fmt.Sprintf(e.Message, args...),
	}
}

func NewError(code, message string) Error {
	return Error{
		ErrCode: code,
		Message: message,
	}
}

var (
	ErrValidation       = NewError("validation", "Validation error: %s")
	ErrResourceNotFound = NewError("resource.not_found", "Resource not found")
)

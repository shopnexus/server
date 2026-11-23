package domain

import "strings"

// The English reasons processing refuses a photo with, which Failed stores
// and clients show. WrongType builds the one that names a type.
const (
	ReasonTooLarge      = "the file is larger than 15 MB"
	ReasonUnreadable    = "the image cannot be read"
	ReasonTooManyPixels = "the image is too large; at most 40 megapixels, 20 at 16 bits a channel"
	ReasonUnknownUpload = "unknown upload"
	ReasonNotAttached   = "the upload is not attached"
)

const wrongTypePrefix = "the file is not a "

// WrongType is the reason for a file whose bytes are not contentType.
func WrongType(contentType string) string { return wrongTypePrefix + contentType + " image" }

// FailureCode is a refused photo's reason as a code, for a client to write
// in its own words.
type FailureCode string

const (
	FailTooLarge      FailureCode = "too_large"
	FailWrongType     FailureCode = "wrong_type"
	FailUnreadable    FailureCode = "unreadable"
	FailTooManyPixels FailureCode = "too_many_pixels"
	FailUnknownUpload FailureCode = "unknown_upload"
	FailNotAttached   FailureCode = "not_attached"
	FailOther         FailureCode = "other"
)

var failureCodes = map[string]FailureCode{
	ReasonTooLarge: FailTooLarge, ReasonUnreadable: FailUnreadable, ReasonTooManyPixels: FailTooManyPixels,
	ReasonUnknownUpload: FailUnknownUpload, ReasonNotAttached: FailNotAttached,
}

// FailureCodeOf reads the code from a stored reason, so a photo refused
// before codes existed has one too: "" for no reason, other for one this
// file lacks.
func FailureCodeOf(reason string) FailureCode {
	if reason == "" {
		return ""
	}
	if c, ok := failureCodes[reason]; ok {
		return c
	}
	if strings.HasPrefix(reason, wrongTypePrefix) {
		return FailWrongType
	}
	return FailOther
}

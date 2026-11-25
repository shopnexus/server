package domain

import (
	"errors"

	"github.com/shopnexus/server/internal/platform/apperr"
)

var (
	ErrNotFound  = apperr.New(apperr.KindNotFound, "listing_read_not_found", "no such read")
	ErrReadLimit = apperr.New(apperr.KindRateLimited, "read_limit_reached",
		"at most 30 video reads, 60 photo reads and 30 link reads in 24 hours")
	// ErrNoSource is a body that names none of video, photo and link; the
	// request validator lets none through.
	ErrNoSource = apperr.New(apperr.KindBadRequest, "invalid_request", "name one of video, photo and link")

	ErrNotALink          = apperr.New(apperr.KindInvalid, "not_a_link", "the text is not an http or https link")
	ErrUnsupportedSource = apperr.New(apperr.KindInvalid, "unsupported_source", "only a listing on chotot.com can be read")
	// ErrNotReading is a change of a read that has ended. The flow never
	// makes one; it is a bug if it shows.
	ErrNotReading = errors.New("assist: the read has ended")
	// ErrWrongSource is a change only another source's read takes, such as
	// a link's photo count on a video read. It is a bug if it shows.
	ErrWrongSource = errors.New("assist: a change for another source's read")
	// ErrWrongFailure is a failure the read's source cannot end with. It
	// is a bug if it shows.
	ErrWrongFailure = errors.New("assist: a failure the read's source cannot end with")
	// ErrNoItem is a model answer that shows no item for sale; the read
	// fails with FailNoItem.
	ErrNoItem = errors.New("assist: the read shows no item for sale")
)

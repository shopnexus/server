package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrNotFound  = apperr.New(apperr.KindNotFound, "video_read_not_found", "no such video read")
	ErrReadLimit = apperr.New(apperr.KindRateLimited, "read_limit_reached", "at most 30 reads of each kind in 24 hours")

	ErrLinkReadNotFound  = apperr.New(apperr.KindNotFound, "link_read_not_found", "no such link read")
	ErrNotALink          = apperr.New(apperr.KindInvalid, "not_a_link", "the text is not an http or https link")
	ErrUnsupportedSource = apperr.New(apperr.KindInvalid, "unsupported_source", "only a listing on chotot.com can be read")
	ErrListingNotFound   = apperr.New(apperr.KindNotFound, "listing_not_found", "Chợ Tốt has no such listing on sale")
	ErrSourceUnavailable = apperr.New(apperr.KindUnavailable, "source_unavailable", "Chợ Tốt did not answer; try again")
	// ErrNotReading is a change of a read that has ended. The flow never
	// makes one; it is a bug if it shows.
	ErrNotReading = errors.New("assist: the video read has ended")
	// ErrWrongSource is a change only another source's read takes, such as
	// a link's photo count on a video read. It is a bug if it shows.
	ErrWrongSource = errors.New("assist: a change for another source's read")
	// ErrWrongFailure is a failure the read's source cannot end with. It
	// is a bug if it shows.
	ErrWrongFailure = errors.New("assist: a failure the read's source cannot end with")
	// ErrNoItem is a model answer that shows no item for sale; the read
	// fails with FailNoItem.
	ErrNoItem = errors.New("assist: the video shows no item for sale")
)

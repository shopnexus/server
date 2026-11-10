package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrNotFound  = apperr.New(apperr.KindNotFound, "video_read_not_found", "no such video read")
	ErrReadLimit = apperr.New(apperr.KindRateLimited, "read_limit_reached", "at most 30 video reads in 24 hours")
	// ErrNotReading is a change of a read that has ended. The flow never
	// makes one; it is a bug if it shows.
	ErrNotReading = errors.New("assist: the video read has ended")
	// ErrNoItem is a model answer that shows no item for sale; the read
	// fails with FailNoItem.
	ErrNoItem = errors.New("assist: the video shows no item for sale")
)

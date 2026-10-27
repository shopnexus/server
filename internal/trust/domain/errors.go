package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrInvalidReview = apperr.New(apperr.KindInvalid, "invalid_review", "the review breaks a rule; see fields")
	ErrLocked        = apperr.New(apperr.KindConflict, "review_locked", "a review changes only within 7 days of its first submission")
	ErrNotFound      = apperr.New(apperr.KindNotFound, "review_not_found", "no review of this order")
	ErrOrderNotFound = apperr.New(apperr.KindNotFound, "order_not_found", "no such order of the caller's")
	ErrNotShipped    = apperr.New(apperr.KindConflict, "order_not_shipped", "an order is reviewed once it is shipped")
)

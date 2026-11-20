package domain

import "github.com/shopnexus/server/internal/platform/apperr"

var (
	ErrInvalidReview = apperr.New(apperr.KindInvalid, "invalid_review", "the review breaks a rule; see fields")
	ErrLocked        = apperr.New(apperr.KindConflict, "review_locked", "a review changes only while it is hidden, within 7 days of its first submission")
	ErrNotFound      = apperr.New(apperr.KindNotFound, "review_not_found", "no such review")
	ErrOrderNotFound = apperr.New(apperr.KindNotFound, "order_not_found", "no such order of the caller's")
	ErrNotDelivered  = apperr.New(apperr.KindConflict, "order_not_delivered", "an order is reviewed once it is completed or failed")
	ErrClosed        = apperr.New(apperr.KindConflict, "review_closed", "a review with a reply no longer changes")
	ErrHidden        = apperr.New(apperr.KindConflict, "review_hidden", "a review is answered once it shows")
	ErrReplied       = apperr.New(apperr.KindConflict, "review_replied", "a review takes one reply")
	ErrOwnReview     = apperr.New(apperr.KindConflict, "own_review", "nobody marks a review they wrote or one about them helpful")

	ErrInvalidReport  = apperr.New(apperr.KindInvalid, "invalid_report", "the report breaks a rule; see fields")
	ErrSelfReport     = apperr.New(apperr.KindInvalid, "self_report", "nobody reports themselves or their own listing")
	ErrReportNotFound = apperr.New(apperr.KindNotFound, "report_not_found", "no such report of the caller's")

	ErrCaseNotFound    = apperr.New(apperr.KindNotFound, "report_case_not_found", "nobody has reported the account")
	ErrCaseNotOpen     = apperr.New(apperr.KindConflict, "report_case_not_open", "the account has no open report case")
	ErrCaseNotResolved = apperr.New(apperr.KindConflict, "report_case_not_resolved", "the account has no resolved report case to reopen")
)

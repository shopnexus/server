package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrInvalidFeedback = apperr.New(apperr.KindInvalid, "invalid_feedback", "a feedback is 1 to 1000 characters")
	ErrFeedbackLimit   = apperr.New(apperr.KindRateLimited, "feedback_limit_reached", "at most 10 feedbacks in 24 hours")
)

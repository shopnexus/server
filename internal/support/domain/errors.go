package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrInvalidTicket = apperr.New(apperr.KindInvalid, "invalid_ticket", "the ticket breaks a rule; see fields")
	ErrNotAllowed    = apperr.New(apperr.KindConflict, "ticket_not_allowed",
		"the order cannot be reported now, or the ticket is in that status already")
	ErrTooMany       = apperr.New(apperr.KindRateLimited, "too_many_tickets", "at most 10 tickets in 24 hours")
	ErrNotFound      = apperr.New(apperr.KindNotFound, "ticket_not_found", "no such ticket")
	ErrOrderNotFound = apperr.New(apperr.KindNotFound, "order_not_found", "no such order of the caller's as buyer")
)

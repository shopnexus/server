package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrNotFound           = apperr.New(apperr.KindNotFound, "conversation_not_found", "no such conversation of the caller's")
	ErrInvalidMessage     = apperr.New(apperr.KindInvalid, "invalid_message", "the message breaks a rule; see fields")
	ErrOwnListing         = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot open a conversation on their own listing")
	ErrListingUnavailable = apperr.New(apperr.KindConflict, "listing_unavailable", "the listing is not on sale")
)

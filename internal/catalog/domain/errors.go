package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrInvalidListing = apperr.New(apperr.KindInvalid, "invalid_listing", "the listing breaks a rule; see fields")
	ErrStateConflict  = apperr.New(apperr.KindConflict, "listing_state_conflict", "the listing's state does not allow this")
	ErrNotFound       = apperr.New(apperr.KindNotFound, "listing_not_found", "no such listing")
	ErrCapReached     = apperr.New(apperr.KindConflict, "listing_cap_reached", "at most 20 listings on sale or hidden")
)

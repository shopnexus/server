// Package domain holds module discovery's rules: for now how many listings
// a person may save.
package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

// MaxSaved is how many listings one person may save.
const MaxSaved = 500

var (
	ErrLimit              = apperr.New(apperr.KindConflict, "saved_limit_reached", "at most 500 saved listings")
	ErrOwnListing         = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot save their own listing")
	ErrListingUnavailable = apperr.New(apperr.KindConflict, "listing_unavailable", "only a listing on sale or being shipped can be saved")
)

// Package domain holds module discovery's rules: how many listings a person
// may save, and how a search query the model read becomes search terms.
package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

// MaxSaved is how many listings one person may save.
const MaxSaved = 500

// MaxHidden is how many hidden listings one person keeps; hiding one more
// shows the oldest again.
const MaxHidden = 1000

var (
	ErrLimit              = apperr.New(apperr.KindConflict, "saved_limit_reached", "at most 500 saved listings")
	ErrOwnListing         = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot save their own listing")
	ErrListingUnavailable = apperr.New(apperr.KindConflict, "listing_unavailable", "only a listing on sale or being shipped can be saved")
	ErrPositionRequired   = apperr.New(apperr.KindBadRequest, "position_required",
		"this sort or radius needs near_lat and near_lng, or near_commune")
	ErrInvalidPosition = apperr.New(apperr.KindBadRequest, "invalid_position",
		"near_lat and near_lng go together, within -90..90 and -180..180, or near_commune names a commune")
	ErrInvalidCursor     = apperr.New(apperr.KindBadRequest, "invalid_cursor", "cursor is a next_cursor this endpoint gave")
	ErrInvalidPriceRange = apperr.New(apperr.KindBadRequest, "invalid_price_range", "price_min is at most price_max")
	ErrSearchConflict    = apperr.New(apperr.KindBadRequest, "search_conflict", "photo goes without q or like, and sort=nearby without q, photo or like")
	ErrSearchUnavailable = apperr.New(apperr.KindUnavailable, "search_unavailable",
		"the photo could not be read for search; try again")
)

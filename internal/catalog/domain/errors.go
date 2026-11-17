package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrInvalidListing    = apperr.New(apperr.KindInvalid, "invalid_listing", "the listing breaks a rule; see fields")
	ErrStateConflict     = apperr.New(apperr.KindConflict, "listing_state_conflict", "the listing's state does not allow this")
	ErrNotFound          = apperr.New(apperr.KindNotFound, "listing_not_found", "no such listing")
	ErrHeld              = apperr.New(apperr.KindConflict, "listing_held", "ShopNexus hid the listing; only ShopNexus puts it back on sale")
	ErrCapReached        = apperr.New(apperr.KindConflict, "listing_cap_reached", "at most 20 listings on sale or hidden")
	ErrDraftNotFound     = apperr.New(apperr.KindNotFound, "draft_not_found", "no such draft of the caller's")
	ErrDraftLimitReached = apperr.New(apperr.KindConflict, "draft_limit_reached", "at most 20 drafts")
	ErrNoCommuneNear     = apperr.New(apperr.KindNotFound, "no_commune_near", "no commune lies within 30 km of the point")
	ErrInvalidPosition   = apperr.New(apperr.KindBadRequest, "invalid_position",
		"near_lat and near_lng go together, within -90..90 and -180..180, or near_commune names a commune")
)

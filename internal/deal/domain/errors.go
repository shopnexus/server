package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrNotFound           = apperr.New(apperr.KindNotFound, "deal_not_found", "no such deal of the caller's")
	ErrMoveNotAllowed     = apperr.New(apperr.KindConflict, "move_not_allowed", "the deal's state or the caller's side does not allow this move")
	ErrListingUnavailable = apperr.New(apperr.KindConflict, "listing_unavailable", "the listing is not on sale")
	ErrPriceChanged       = apperr.New(apperr.KindConflict, "price_changed", "the price to accept has changed")
	ErrInvalidAmount      = apperr.New(apperr.KindInvalid, "invalid_amount", "the amount is outside the offer range; see fields")
	ErrOwnListing         = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot open a deal on their own listing")
	ErrChanged            = apperr.New(apperr.KindConflict, "deal_changed", "the other side moved first; read the deal again")
	ErrPhoneRequired      = apperr.New(apperr.KindForbidden, "phone_required", "add a verified phone number before dealing")
)

package domain

import "github.com/shopnexus/server/internal/platform/apperr"

var (
	ErrOrdersOpen         = apperr.New(apperr.KindConflict, "orders_open", "an order of the account is pending or shipped")
	ErrNotFound           = apperr.New(apperr.KindNotFound, "deal_not_found", "no such deal of the caller's")
	ErrOrderNotFound      = apperr.New(apperr.KindNotFound, "order_not_found", "no such order of the caller's")
	ErrMoveNotAllowed     = apperr.New(apperr.KindConflict, "move_not_allowed", "the deal's state or the caller's side does not allow this move")
	ErrListingUnavailable = apperr.New(apperr.KindConflict, "listing_unavailable", "the listing is not on sale")
	ErrPriceChanged       = apperr.New(apperr.KindConflict, "price_changed", "the price to accept has changed")
	ErrInvalidAmount      = apperr.New(apperr.KindInvalid, "invalid_amount", "the amount is outside the offer range; see fields")
	ErrOwnListing         = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot open a deal on their own listing")
	ErrChanged            = apperr.New(apperr.KindConflict, "deal_changed", "the other side moved first; read the deal again")
	ErrInvalidShipment    = apperr.New(apperr.KindInvalid, "invalid_shipment", "the shipment breaks a rule; see fields")
	// ErrShipmentUnknown is a tracking code the carrier does not know, on a
	// shipment being created or corrected.
	ErrShipmentUnknown = apperr.New(apperr.KindInvalid, "shipment_not_found", "the carrier does not know the tracking code")
	// ErrShipmentNotFound is a tracking code nothing knows: the carrier, on
	// a lookup, or every order, on a carrier's report.
	ErrShipmentNotFound = apperr.New(apperr.KindNotFound, "shipment_not_found", "no such shipment")
	ErrInvalidStatus    = apperr.New(apperr.KindInvalid, "invalid_status", "the status is not pending_pickup, in_transit, delivered or failed")
	// ErrCarrierUnavailable is a stack without a carrier (CARRIER=none):
	// no shipment can be made, read or corrected there.
	ErrCarrierUnavailable = apperr.New(apperr.KindUnavailable, "carrier_unavailable", "no carrier makes or reads shipments here")
	// ErrCarrierNotLinked is a carrier the seller has not linked: module
	// shipping reads and makes shipments only with the seller's own account.
	ErrCarrierNotLinked = apperr.New(apperr.KindConflict, "carrier_not_linked", "the caller has not linked that carrier")
	// ErrCarrierAddressUnknown is a recipient or pickup address the carrier
	// does not deliver to or does not know.
	ErrCarrierAddressUnknown = apperr.New(apperr.KindInvalid, "carrier_address_unknown", "the carrier does not know the address")
	// errFailedParcel is a parcel the carrier reports failed, which no
	// order ships on.
	errFailedParcel = ErrInvalidShipment.WithField("tracking_code", "the carrier reports this parcel failed")
	ErrCodLimit     = apperr.New(apperr.KindConflict, "cod_limit_reached", "at most 3 open COD orders")
	// ErrSellerAway refuses a buyer's offer, accept or COD request while
	// the seller is in away mode.
	ErrSellerAway = apperr.New(apperr.KindConflict, "seller_away", "the seller is away; try again after they are back")
)

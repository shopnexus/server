package domain

import "github.com/shopnexus/server/internal/platform/apperr"

var (
	ErrPickupNotConfigured = apperr.New(apperr.KindConflict, "pickup_not_configured", "save a pickup address and contact phone first")
	ErrInvalidSettings     = apperr.New(apperr.KindInvalid, "invalid_shipping_settings", "the shipping settings break a rule; see fields")
	ErrNotLinked           = apperr.New(apperr.KindConflict, "carrier_not_linked", "the caller has not linked that carrier")
	ErrLoginFailed         = apperr.New(apperr.KindInvalid, "carrier_login_failed", "the carrier refused the account")
	ErrInvalidCarrier      = apperr.New(apperr.KindInvalid, "invalid_carrier", "the carrier is not ghtk or ghn")
	ErrShipmentsOpen       = apperr.New(apperr.KindConflict, "shipments_open", "a shipment at that carrier is still open")
	ErrAccountNotFound     = apperr.New(apperr.KindNotFound, "carrier_account_not_found", "no account linked at that carrier")
	// ErrUnavailable is a stack without a carrier, CARRIER=none. It shares
	// its code with deal's error of the same meaning.
	ErrUnavailable    = apperr.New(apperr.KindUnavailable, "carrier_unavailable", "no carrier makes or reads shipments here")
	ErrAddressUnknown = apperr.New(apperr.KindInvalid, "carrier_address_unknown", "the carrier does not know the address")
	ErrPhoneUnknown   = apperr.New(apperr.KindInvalid, "carrier_phone_unknown", "no account at the carrier has this phone")
	ErrOTPInvalid     = apperr.New(apperr.KindInvalid, "carrier_otp_invalid", "the code is wrong or expired")
	// ErrCarrierRefused is a request the carrier refused; its detail
	// reason carries the carrier's words.
	ErrCarrierRefused = apperr.New(apperr.KindInvalid, "carrier_refused", "the carrier refused the request; see reason")
	// ErrRelinkNeeded is a token that does not open under this
	// environment's key: the seller links the carrier again.
	ErrRelinkNeeded = apperr.New(apperr.KindConflict, "carrier_relink_needed", "link the carrier again")
	// ErrInvalidShipment is a shipment to price that breaks a rule; deal
	// answers a shipment to make with the same code.
	ErrInvalidShipment = apperr.New(apperr.KindInvalid, "invalid_shipment", "the shipment breaks a rule; see fields")
	// ErrInvalidStep is a step the dev endpoint does not know.
	ErrInvalidStep = apperr.New(apperr.KindInvalid, "invalid_step", "the step is not one of ShipmentStep")
)

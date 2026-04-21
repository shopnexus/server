package accountmodel

import (
	"errors"
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the account module.
var (
	ErrInvalidCredentials = sharedmodel.NewError(http.StatusUnauthorized, "invalid_credentials", "Invalid credentials provided")
	ErrAccountNotFound    = sharedmodel.NewError(http.StatusNotFound, "account_not_found", "Account not found")
	ErrMissingIdentifier  = sharedmodel.NewError(
		http.StatusBadRequest,
		"missing_identifier",
		"At least one of username, email, or phone must be provided",
	)
	ErrEmailRequiredForOAuth = sharedmodel.NewError(
		http.StatusBadRequest,
		"email_required_for_oauth",
		"Email is required when password is not provided",
	)
	ErrContactNotFound  = sharedmodel.NewError(http.StatusNotFound, "contact_not_found", "The contact could not be found")
	ErrNoDefaultContact = sharedmodel.NewError(
		http.StatusNotFound,
		"no_default_contact",
		"Some accounts do not have a default contact address",
	)
	ErrCannotDeleteLastContact  = sharedmodel.NewError(http.StatusConflict, "cannot_delete_last_contact", "Cannot delete the only contact address")
	ErrCardPaymentNotConfigured = sharedmodel.NewError(http.StatusNotImplemented, "card_payment_not_configured", "card payment not configured")

	ErrInvalidCountry                = sharedmodel.NewError(http.StatusBadRequest, "invalid_country", "invalid country: %v")
	ErrContactAddressCountryMismatch = sharedmodel.NewError(
		http.StatusBadRequest,
		"address_country_mismatch",
		"address resolves to %s, profile country is %s",
	)
	ErrWalletNotEmpty = sharedmodel.NewError(
		http.StatusConflict,
		"wallet_not_empty",
		"wallet balance is %d, must be zero to change country",
	)
)

var (
	ErrUnsupportedCurrency = errors.New("account: unsupported currency")
	ErrForbidden           = errors.New("account: forbidden")
)

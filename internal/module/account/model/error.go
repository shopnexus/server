package accountmodel

import sharedmodel "shopnexus-server/internal/shared/model"

// Sentinel errors for the account module.
var (
	ErrInvalidCredentials     = sharedmodel.NewError("auth.invalid_credentials", "Invalid credentials provided")
	ErrAccountNotFound        = sharedmodel.NewError("auth.account_not_found", "Account not found")
	ErrMissingIdentifier      = sharedmodel.NewError("auth.missing_identifier", "At least one of username, email, or phone must be provided")
	ErrEmailRequiredForOAuth  = sharedmodel.NewError("account.email_required", "Email is required when password is not provided")
	ErrUnsupportedAccountType = sharedmodel.NewError("account.unsupported_type", "The account type is not supported")
	ErrContactNotFound        = sharedmodel.NewError("account.contact_not_found", "The contact could not be found")
	ErrNoDefaultContact       = sharedmodel.NewError("account.no_default_contact", "Some accounts do not have a default contact address")
)

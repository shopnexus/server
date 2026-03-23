package accountmodel

import commonmodel "shopnexus-server/internal/shared/model"

var (
	ErrInvalidCredentials     = commonmodel.NewError("auth.invalid_credentials", "Invalid credentials provided")
	ErrAccountNotFound        = commonmodel.NewError("auth.account_not_found", "Account not found")
	ErrMissingIdentifier      = commonmodel.NewError("auth.missing_identifier", "At least one of username, email, or phone must be provided")
	ErrEmailRequiredForOAuth  = commonmodel.NewError("account.email_required", "Email is required when password is not provided")
	ErrUnsupportedAccountType = commonmodel.NewError("account.unsupported_type", "The account type is not supported")
	ErrContactNotFound        = commonmodel.NewError("account.contact_not_found", "The contact could not be found")
	ErrNoDefaultContact       = commonmodel.NewError("account.no_default_contact", "Some accounts do not have a default contact address")
)

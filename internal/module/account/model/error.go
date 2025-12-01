package accountmodel

import commonmodel "shopnexus-remastered/internal/shared/model"

var (
	ErrInvalidCredentials = commonmodel.NewError("auth.invalid_credentials", "Invalid credentials provided")
	ErrAccountNotFound    = commonmodel.NewError("auth.account_not_found", "Account not found")
	ErrMissingIdentifier  = commonmodel.NewError("auth.missing_identifier", "At least one of username, email, or phone must be provided")
)

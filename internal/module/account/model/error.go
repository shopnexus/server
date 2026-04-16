package accountmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the account module.
var (
	ErrInvalidCredentials = sharedmodel.NewError(http.StatusUnauthorized, "Invalid credentials provided")
	ErrAccountNotFound    = sharedmodel.NewError(http.StatusNotFound, "Account not found")
	ErrMissingIdentifier  = sharedmodel.NewError(
		http.StatusBadRequest,
		"At least one of username, email, or phone must be provided",
	)
	ErrEmailRequiredForOAuth = sharedmodel.NewError(
		http.StatusBadRequest,
		"Email is required when password is not provided",
	)
	ErrContactNotFound  = sharedmodel.NewError(http.StatusNotFound, "The contact could not be found")
	ErrNoDefaultContact = sharedmodel.NewError(
		http.StatusNotFound,
		"Some accounts do not have a default contact address",
	)
	ErrCannotDeleteLastContact = sharedmodel.NewError(http.StatusConflict, "Cannot delete the only contact address")
)

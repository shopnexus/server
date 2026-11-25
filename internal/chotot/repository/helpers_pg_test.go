package repository_test

import (
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// domainErr is an invalid_listing refusal of the price.
func domainErr() *apperr.Error {
	return catalogdomain.ErrInvalidListing.WithField("price", apperr.OutOfRange, "too low")
}

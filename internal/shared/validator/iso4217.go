package validator

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

var iso4217Re = regexp.MustCompile(`^[A-Z]{3}$`)

// Iso4217 validates that a field is a 3-letter uppercase ISO 4217 code.
// Format-only; whitelist validation against config.Exchange.Supported
// is performed at the biz layer.
func Iso4217(fl validator.FieldLevel) bool {
	return iso4217Re.MatchString(fl.Field().String())
}

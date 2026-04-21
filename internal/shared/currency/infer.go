package currency

import (
	"errors"
	"fmt"

	xcurrency "golang.org/x/text/currency"
	"golang.org/x/text/language"
)

// ErrNoCurrencyForRegion signals that the region is valid ISO 3166 but has no
// canonical ISO 4217 currency (e.g. Antarctica).
var ErrNoCurrencyForRegion = errors.New("no canonical currency for region")

// Infer returns the canonical ISO 4217 currency code for an ISO 3166-1 alpha-2
// country code. Eurozone countries all map to EUR.
func Infer(countryCode string) (string, error) {
	region, err := language.ParseRegion(countryCode)
	if err != nil {
		return "", fmt.Errorf("parse region %q: %w", countryCode, err)
	}
	unit, ok := xcurrency.FromRegion(region)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoCurrencyForRegion, countryCode)
	}
	return unit.String(), nil
}

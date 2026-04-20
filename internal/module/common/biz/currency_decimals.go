package commonbiz

import "golang.org/x/text/currency"

// decimalsFor returns the ISO 4217 minor-unit exponent, defaulting to 2
// when the currency is unknown. Backed by CLDR data via x/text/currency,
// so it covers all ISO 4217 codes (e.g. VND=0, USD=2, BHD=3, CLF=4).
func decimalsFor(code string) int {
	unit, err := currency.ParseISO(code)
	if err != nil {
		return 2
	}
	scale, _ := currency.Standard.Rounding(unit)
	return scale
}

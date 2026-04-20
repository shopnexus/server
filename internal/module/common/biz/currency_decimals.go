package commonbiz

// currencyDecimals is the ISO 4217 minor-unit exponent for supported
// currencies. Used for smallest-unit <-> major-unit conversion during
// cross-currency math. Callers unaware of a currency MUST default to 2.
var currencyDecimals = map[string]int{
	"VND": 0, "USD": 2, "JPY": 0, "KRW": 0, "EUR": 2,
	"GBP": 2, "CNY": 2, "SGD": 2, "THB": 2, "AUD": 2,
}

// decimalsFor returns the ISO 4217 minor-unit exponent, defaulting to 2.
func decimalsFor(currency string) int {
	if d, ok := currencyDecimals[currency]; ok {
		return d
	}
	return 2
}

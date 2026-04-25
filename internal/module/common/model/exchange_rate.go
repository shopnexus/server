package commonmodel

import (
	"time"

	"github.com/shopspring/decimal"
)

// ExchangeRateSnapshot is the FE-facing shape of the exchange rate table.
// Base is always USD in current deployment. Rates map ISO 4217 targets
// to multipliers: amount_in_target = amount_in_base * Rates[target].
// Rates does NOT include Base itself (identity handled client-side).
//
// Rates values are decimal.Decimal — JSON-marshaled as strings (e.g.
// "25000.5") to preserve precision across language boundaries.
type ExchangeRateSnapshot struct {
	Base      string                     `json:"base"`
	Rates     map[string]decimal.Decimal `json:"rates"`
	FetchedAt time.Time                  `json:"fetched_at"`
	Supported []string                   `json:"supported"`
}

// Package exchange provides currency exchange rate lookup against an
// external provider. Implementations must be safe for concurrent use.
package exchange

import (
	"context"
	"time"
)

// Snapshot is one immutable rate lookup result against a base currency.
// Rates map target ISO 4217 codes to multipliers: amount_in_target =
// amount_in_base * Rates[target]. The base currency itself is NOT
// included in Rates (caller handles identity as 1.0).
type Snapshot struct {
	Base      string
	Rates     map[string]float64
	FetchedAt time.Time
}

// Client fetches latest exchange rates from an upstream provider.
type Client interface {
	FetchLatest(ctx context.Context, base string, targets []string) (Snapshot, error)
}

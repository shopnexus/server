package commonbiz

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	restate "github.com/restatedev/sdk-go"

	commonmodel "shopnexus-server/internal/module/common/model"
)

// exchangeRateCacheTTL is longer than the refresh interval so stale rates
// survive provider outages between successful refreshes.
const exchangeRateCacheTTL = 24 * time.Hour

func exchangeRateCacheKey(base string) string {
	return fmt.Sprintf("common:exchange:rates:%s", base)
}

// GetExchangeRatesParams is an empty envelope required by the Restate
// ingress client — zero-arg handlers reject requests with a JSON
// content-type, so we always send an empty object.
type GetExchangeRatesParams struct{}

// ConvertAmountParams: amount in smallest unit of From, converted to
// smallest unit of To.
type ConvertAmountParams struct {
	Amount   int64
	From, To string
}

// ConvertAmountPure converts amount through the USD base. ratesFromUSD
// maps target currency to "1 USD = rate target". Returns the original
// amount unchanged when a rate is missing (fail-open; callers display
// original currency). Exported for testability without cache setup.
func ConvertAmountPure(amount int64, from, to string, ratesFromUSD map[string]float64) int64 {
	if from == to {
		return amount
	}
	rateFrom := 1.0
	if from != "USD" {
		r, ok := ratesFromUSD[from]
		if !ok || r <= 0 {
			return amount
		}
		rateFrom = r
	}
	rateTo := 1.0
	if to != "USD" {
		r, ok := ratesFromUSD[to]
		if !ok || r <= 0 {
			return amount
		}
		rateTo = r
	}
	decFrom := decimalsFor(from)
	decTo := decimalsFor(to)
	majorFrom := float64(amount) / math.Pow10(decFrom)
	majorUSD := majorFrom / rateFrom
	majorTo := majorUSD * rateTo
	return int64(math.Round(majorTo * math.Pow10(decTo)))
}

// GetExchangeRates reads the latest snapshot from cache. On cache miss
// (before the first cron refresh completes) or cache error, returns an
// empty Rates map with correct metadata — callers fail-open.
func (b *CommonHandler) GetExchangeRates(ctx restate.Context, _ GetExchangeRatesParams) (commonmodel.ExchangeRateSnapshot, error) {
	base := b.config.App.Exchange.Base

	var snap commonmodel.ExchangeRateSnapshot
	if err := b.cache.Get(ctx, exchangeRateCacheKey(base), &snap); err != nil {
		slog.Warn("exchange cache get failed", "base", base, "err", err)
	}

	// Cache miss leaves snap at zero value — Rates will be nil.
	if snap.Rates == nil {
		snap.Rates = map[string]float64{}
	}
	snap.Base = base
	snap.Supported = b.config.App.Exchange.Supported
	return snap, nil
}

// ConvertAmount: BE helper for cross-currency math (filter, analytics).
func (b *CommonHandler) ConvertAmount(ctx restate.Context, p ConvertAmountParams) (int64, error) {
	snap, err := b.GetExchangeRates(ctx, GetExchangeRatesParams{})
	if err != nil {
		return 0, err
	}
	return ConvertAmountPure(p.Amount, p.From, p.To, snap.Rates), nil
}

// IsSupportedCurrency checks against the config whitelist.
// Returns an error tuple to conform to the Restate proxy calling convention
// for interface methods; lookup itself never fails.
func (b *CommonHandler) IsSupportedCurrency(_ restate.Context, currency string) (bool, error) {
	return slices.Contains(b.config.App.Exchange.Supported, currency), nil
}

// RefreshExchangeRates fetches the latest rates from the provider and
// overwrites the cached snapshot. Invoked by SetupExchangeCron on startup
// and on each ticker.
func (b *CommonHandler) RefreshExchangeRates(ctx context.Context) error {
	if b.exchange == nil {
		return fmt.Errorf("exchange: no provider configured")
	}
	base := b.config.App.Exchange.Base
	targets := make([]string, 0, len(b.config.App.Exchange.Supported))
	for _, c := range b.config.App.Exchange.Supported {
		if c != base {
			targets = append(targets, c)
		}
	}
	fetched, err := b.exchange.FetchLatest(ctx, base, targets)
	if err != nil {
		return fmt.Errorf("refresh rates: fetch: %w", err)
	}

	snap := commonmodel.ExchangeRateSnapshot{
		Base:      base,
		Rates:     fetched.Rates,
		FetchedAt: fetched.FetchedAt,
	}
	if err := b.cache.Set(ctx, exchangeRateCacheKey(base), snap, exchangeRateCacheTTL); err != nil {
		return fmt.Errorf("refresh rates: cache set: %w", err)
	}
	return nil
}

// SetupExchangeCron starts the rate refresh goroutine. Mirrors the
// catalog search sync pattern. Safe to call once; non-blocking.
func (b *CommonHandler) SetupExchangeCron() {
	interval := b.config.App.Exchange.RefreshInterval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	go b.exchangeCronLoop(context.Background(), interval)
}

func (b *CommonHandler) exchangeCronLoop(ctx context.Context, interval time.Duration) {
	slog.Info("exchange rate cron starting", "interval", interval)
	if err := b.RefreshExchangeRates(ctx); err != nil {
		slog.Warn("initial exchange refresh failed", "err", err)
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := b.RefreshExchangeRates(ctx); err != nil {
				slog.Warn("periodic exchange refresh failed", "err", err)
			}
		}
	}
}

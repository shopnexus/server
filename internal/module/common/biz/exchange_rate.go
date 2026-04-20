package commonbiz

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
)

// ConvertAmountParams: amount in smallest unit of From, converted to
// smallest unit of To.
type ConvertAmountParams struct {
	Amount   int64
	From, To string
}

// ConvertAmountPure converts amount through the USD base. ratesFromUSD
// maps target currency to "1 USD = rate target". Returns the original
// amount unchanged when a rate is missing (fail-open; callers display
// original currency). Exported for testability without DB setup.
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

// GetExchangeRates reads rates from DB and returns the FE-facing snapshot.
func (b *CommonHandler) GetExchangeRates(ctx context.Context) (commonmodel.ExchangeRateSnapshot, error) {
	rows, err := b.storage.Querier().GetExchangeRatesByBase(ctx, b.config.App.Exchange.Base)
	if err != nil {
		return commonmodel.ExchangeRateSnapshot{}, fmt.Errorf("get exchange rates: %w", err)
	}
	rates := make(map[string]float64, len(rows))
	var latest *time.Time
	for _, r := range rows {
		f, _ := r.Rate.Float64Value()
		rates[r.Target] = f.Float64
		if latest == nil || r.FetchedAt.After(*latest) {
			t := r.FetchedAt
			latest = &t
		}
	}
	return commonmodel.ExchangeRateSnapshot{
		Base:      b.config.App.Exchange.Base,
		Rates:     rates,
		FetchedAt: latest,
		Supported: b.config.App.Exchange.Supported,
	}, nil
}

// ConvertAmount: BE helper for cross-currency math (filter, analytics).
func (b *CommonHandler) ConvertAmount(ctx context.Context, p ConvertAmountParams) (int64, error) {
	snap, err := b.GetExchangeRates(ctx)
	if err != nil {
		return 0, err
	}
	return ConvertAmountPure(p.Amount, p.From, p.To, snap.Rates), nil
}

// IsSupportedCurrency checks against the config whitelist.
// Returns an error tuple to conform to the Restate proxy calling convention
// for interface methods; lookup itself never fails.
func (b *CommonHandler) IsSupportedCurrency(_ context.Context, currency string) (bool, error) {
	return slices.Contains(b.config.App.Exchange.Supported, currency), nil
}

// RefreshExchangeRates fetches latest rates and upserts them.
// Invoked by SetupExchangeCron on startup and on each ticker.
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
	snap, err := b.exchange.FetchLatest(ctx, base, targets)
	if err != nil {
		return fmt.Errorf("refresh rates: fetch: %w", err)
	}

	for target, rate := range snap.Rates {
		if err := b.storage.Querier().UpsertExchangeRate(ctx, commondb.UpsertExchangeRateParams{
			Base:      base,
			Target:    target,
			Rate:      pgNumericFromFloat(rate),
			FetchedAt: snap.FetchedAt,
		}); err != nil {
			slog.Warn("upsert exchange rate failed",
				"base", base, "target", target, "err", err)
		}
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

// pgNumericFromFloat converts a float64 to pgtype.Numeric via string
// formatting — avoids loss of precision that direct binary encoding
// would incur on non-terminating decimals like 1/3.
func pgNumericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', -1, 64))
	return n
}

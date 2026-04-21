package commonbiz

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"shopnexus-server/internal/provider/geocoding"
	sharedmodel "shopnexus-server/internal/shared/model"

	restate "github.com/restatedev/sdk-go"
)

// forwardGeocodeCacheTTL is long because address -> country mappings rarely
// change; the cache is the primary defence against Nominatim rate limits.
const forwardGeocodeCacheTTL = 30 * 24 * time.Hour

func forwardGeocodeCacheKey(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(address))))
	return "geocode:forward:" + hex.EncodeToString(sum[:])[:16]
}

type ReverseGeocodeParams struct {
	Latitude  float64 `validate:"required"`
	Longitude float64 `validate:"required"`
}

func (b *CommonHandler) ReverseGeocode(ctx restate.Context, params ReverseGeocodeParams) (geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) (geocoding.Result, error) {
		result, err := b.geocoder.ReverseGeocode(ctx, params.Latitude, params.Longitude)
		if err != nil {
			return geocoding.Result{}, sharedmodel.WrapErr("reverse geocode", err)
		}
		return result, nil
	})
}

type ForwardGeocodeParams struct {
	Address string `validate:"required"`
}

// ForwardGeocode resolves a free-form address to coordinates and country.
// Cached by lowercased address for 30 days to limit Nominatim load and stay
// within its 1 req/sec public rate limit.
func (b *CommonHandler) ForwardGeocode(ctx restate.Context, params ForwardGeocodeParams) (geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) (geocoding.Result, error) {
		cacheKey := forwardGeocodeCacheKey(params.Address)

		var cached geocoding.Result
		if err := b.cache.Get(ctx, cacheKey, &cached); err == nil && cached.Address != "" {
			return cached, nil
		}

		result, err := b.geocoder.ForwardGeocode(ctx, params.Address)
		if err != nil {
			return geocoding.Result{}, sharedmodel.WrapErr("forward geocode", err)
		}

		if result.Address != "" {
			if err := b.cache.Set(ctx, cacheKey, result, forwardGeocodeCacheTTL); err != nil {
				slog.Warn("forward geocode cache set failed", "err", err)
			}
		}

		return result, nil
	})
}

// ResolveCountry geocodes the address and returns the ISO 3166-1 alpha-2
// country code (uppercase). Returns a terminal 400 error if the address is
// blank, geocoding fails, or no country was resolved.
func (b *CommonHandler) ResolveCountry(ctx restate.Context, address string) (string, error) {
	if strings.TrimSpace(address) == "" {
		return "", sharedmodel.NewError(http.StatusBadRequest, "address is empty").Terminal()
	}
	result, err := b.ForwardGeocode(ctx, ForwardGeocodeParams{Address: address})
	if err != nil {
		return "", sharedmodel.WrapErr("resolve address country", err)
	}
	if result.CountryCode == "" {
		return "", sharedmodel.NewError(
			http.StatusBadRequest,
			"could not verify address country (no country in geocode result)",
		).Terminal()
	}
	return result.CountryCode, nil
}

type SearchGeocodeParams struct {
	Query string `validate:"required,min=2"`
	Limit int
}

func (b *CommonHandler) SearchGeocode(ctx restate.Context, params SearchGeocodeParams) ([]geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) ([]geocoding.Result, error) {
		results, err := b.geocoder.Search(ctx, params.Query, params.Limit)
		if err != nil {
			return nil, sharedmodel.WrapErr("search geocode", err)
		}
		return results, nil
	})
}

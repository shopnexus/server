package geocoding

import (
	"context"
	"errors"
)

// ErrNoResults is returned when the provider cannot resolve the given input
// (address or coordinates) to any location. Callers can use errors.Is to map
// this into a terminal/400 error so that Restate stops retrying.
var ErrNoResults = errors.New("geocoding: no results")

// Result holds the geocoded address and coordinates.
type Result struct {
	Address     string  `json:"address"`
	CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2 uppercase, empty if unresolved.
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// Client is the interface for geocoding services.
// Swap implementations (Nominatim, Google Maps, etc.) without changing callers.
type Client interface {
	// ReverseGeocode converts coordinates to an address.
	ReverseGeocode(ctx context.Context, lat, lng float64) (Result, error)
	// ForwardGeocode converts an address string to coordinates.
	ForwardGeocode(ctx context.Context, address string) (Result, error)
	// Search returns location suggestions matching a partial query.
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

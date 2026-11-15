package service

import (
	"context"
	"fmt"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
)

func (c *Catalog) Provinces(ctx context.Context) ([]domain.Province, error) {
	ps, err := c.regions.Provinces(ctx)
	if err != nil {
		return nil, fmt.Errorf("read provinces: %w", err)
	}
	return ps, nil
}

// Communes returns a province's communes by display name; an unknown province has
// none.
func (c *Catalog) Communes(ctx context.Context, provinceCode string) ([]domain.Commune, error) {
	cs, err := c.regions.Communes(ctx, provinceCode)
	if err != nil {
		return nil, fmt.Errorf("read communes: %w", err)
	}
	return cs, nil
}

// Places reads the communes among codes with their provinces in one query.
func (c *Catalog) Places(ctx context.Context, codes []string) (map[string]Location, error) {
	rows, err := c.regions.Places(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("get communes: %w", err)
	}
	out := make(map[string]Location, len(rows))
	for _, p := range rows {
		out[p.Code] = locationOf(p)
	}
	return out, nil
}

// Origin is the point near names: its GPS point, or its commune's point.
// The zero Near is no position, nil. A GPS point needs both coordinates in
// range, and the commune must exist; otherwise domain.ErrInvalidPosition.
func (c *Catalog) Origin(ctx context.Context, near domain.Near) (*domain.Point, error) {
	switch {
	case near.Lat != 0 || near.Lng != 0:
		if near.Lat == 0 || near.Lng == 0 || near.Lat < -90 || near.Lat > 90 || near.Lng < -180 || near.Lng > 180 {
			return nil, domain.ErrInvalidPosition
		}
		return &domain.Point{Lat: near.Lat, Lng: near.Lng}, nil
	case near.Commune != "":
		places, err := c.Places(ctx, []string{near.Commune})
		if err != nil {
			return nil, fmt.Errorf("read the commune: %w", err)
		}
		loc, ok := places[near.Commune]
		if !ok {
			return nil, domain.ErrInvalidPosition
		}
		return &domain.Point{Lat: loc.Lat, Lng: loc.Lng}, nil
	default:
		return nil, nil //nolint:nilnil // no position is a valid answer, not a missing one
	}
}

func (c *Catalog) Locate(ctx context.Context, p domain.Point) (Location, error) {
	place, err := c.regions.Nearest(ctx, p)
	if err != nil {
		return Location{}, fmt.Errorf("find the commune: %w", err)
	}
	if domain.Distance(p, domain.Point{Lat: place.Lat, Lng: place.Lng}) > domain.MaxGPSKm {
		return Location{}, domain.ErrNoCommuneNear
	}
	return locationOf(place), nil
}

func locationOf(p repository.Place) Location {
	return Location{
		Commune:  Region{Code: p.Code, DisplayName: p.DisplayName},
		Province: Region{Code: p.ProvinceCode, DisplayName: p.Province},
		Lat:      p.Lat, Lng: p.Lng,
	}
}

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

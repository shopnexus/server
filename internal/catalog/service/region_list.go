package service

import (
	"context"
	"fmt"

	"github.com/shopnexus/server2/internal/catalog/domain"
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

package service

import (
	"context"
	"fmt"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Categories returns the category tree named in t: the roots in order, then
// each root's subcategories in order. Text a model reads takes Vietnamese.
func (c *Catalog) Categories(ctx context.Context, t locale.Tag) ([]domain.Category, error) {
	cats, err := c.categories.Tree(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	return cats, nil
}

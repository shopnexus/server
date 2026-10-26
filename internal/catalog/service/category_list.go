package service

import (
	"context"
	"fmt"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// Categories returns the category tree: the roots in order, then each
// root's subcategories in order.
func (c *Catalog) Categories(ctx context.Context) ([]domain.Category, error) {
	cats, err := c.categories.Tree(ctx)
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	return cats, nil
}

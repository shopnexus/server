package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/locale"
)

// PriceQuery is a listing input a price sample is drawn for: its subcategory, its
// condition and its vectors, embedded by the caller with the models behind
// catalog's versions. Exclude is the seller, whose own listings are left
// out. TextMin and PhotoMin are the cosine floors; Now is when the windows
// end.
type PriceQuery struct {
	Exclude           uuid.UUID
	CategoryCode      string
	Condition         domain.Condition
	Text, Photo       []float32
	TextMin, PhotoMin float64
	Now               time.Time
}

// PriceSample is one similar listing's price and whether it sold.
type PriceSample = domain.PriceSample

func (c *Catalog) PriceSample(ctx context.Context, q PriceQuery) ([]PriceSample, error) {
	samples, err := c.d.Vectors.PriceSample(ctx, repository.PriceFilter{
		Exclude: q.Exclude, CategoryCode: q.CategoryCode, Condition: q.Condition,
		Text: q.Text, TextVersion: c.d.TextVersion, TextMin: q.TextMin,
		Photo: q.Photo, PhotoVersion: c.d.ImageVersion, PhotoMin: q.PhotoMin,
		SoldSince: q.Now.Add(-domain.PriceSoldWindow), OnSaleSince: q.Now.Add(-domain.PriceOnSaleWindow),
		Limit: domain.PriceSampleSize,
	})
	if err != nil {
		return nil, fmt.Errorf("draw a price sample: %w", err)
	}
	return samples, nil
}

func (c *Catalog) EmbeddingText(ctx context.Context, d domain.ListingInput) (string, error) {
	tree, err := c.d.Categories.Tree(ctx, locale.Vietnamese)
	if err != nil {
		return "", fmt.Errorf("read categories: %w", err)
	}
	name := d.CategoryCode
	for _, cat := range tree {
		if cat.Code == d.CategoryCode {
			name = cat.DisplayName
		}
	}
	return d.EmbeddingText(name), nil
}

func (c *Catalog) KeySpecs(ctx context.Context, categoryCode string, specs []domain.Spec) ([]domain.Spec, error) {
	_, names, err := c.d.Categories.Leaf(ctx, categoryCode)
	if err != nil {
		return nil, fmt.Errorf("read the category's specs: %w", err)
	}
	return domain.KeySpecs(specs, names), nil
}

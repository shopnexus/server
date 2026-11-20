package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

// sitemapPartSize is how many addresses one sitemap file of the website
// holds, well under the 50,000 a sitemap may.
const sitemapPartSize = 10_000

// Sitemap counts the parts of the public listings and of their sellers.
func (c *Catalog) Sitemap(ctx context.Context) (SitemapView, error) {
	listings, err := c.listings.ListingParts(ctx, sitemapPartSize)
	if err != nil {
		return SitemapView{}, fmt.Errorf("count listing parts: %w", err)
	}
	sellers, err := c.listings.SellerParts(ctx, sitemapPartSize)
	if err != nil {
		return SitemapView{}, fmt.Errorf("count seller parts: %w", err)
	}
	v := SitemapView{Listings: []SitemapPartView{}, Sellers: []SitemapPartView{}}
	for _, p := range listings {
		v.Listings = append(v.Listings, partView(p))
	}
	for _, p := range sellers {
		v.Sellers = append(v.Sellers, partView(p))
	}
	return v, nil
}

func partView(p repository.SitemapPart) SitemapPartView {
	v := SitemapPartView{UpdatedAt: p.UpdatedAt}
	if p.Cursor != (uuid.UUID{}) {
		v.Cursor = new(p.Cursor)
	}
	return v
}

// SitemapListings reads one part of the public listings as web addresses.
func (c *Catalog) SitemapListings(ctx context.Context, cursor uuid.UUID) ([]SitemapListingView, error) {
	rows, err := c.listings.SitemapListings(ctx, page.Request{Cursor: cursor}.Before(), sitemapPartSize)
	if err != nil {
		return nil, fmt.Errorf("list sitemap listings: %w", err)
	}
	v := make([]SitemapListingView, 0, len(rows))
	for _, l := range rows {
		v = append(v, SitemapListingView{Slug: domain.Slug(l.Title, l.Code), UpdatedAt: l.UpdatedAt})
	}
	return v, nil
}

// SitemapSellers reads one part of the sellers with a public listing.
func (c *Catalog) SitemapSellers(ctx context.Context, cursor uuid.UUID) ([]SitemapSellerView, error) {
	rows, err := c.listings.SitemapSellers(ctx, page.Request{Cursor: cursor}.Before(), sitemapPartSize)
	if err != nil {
		return nil, fmt.Errorf("list sitemap sellers: %w", err)
	}
	v := make([]SitemapSellerView, 0, len(rows))
	for _, s := range rows {
		v = append(v, SitemapSellerView(s))
	}
	return v, nil
}

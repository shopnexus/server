package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func (s pgListings) ListingParts(ctx context.Context, size int) ([]SitemapPart, error) {
	return s.parts(ctx, `SELECT id AS key, updated_at FROM listings WHERE status = ANY(@public)`, size)
}

func (s pgListings) SellerParts(ctx context.Context, size int) ([]SitemapPart, error) {
	return s.parts(ctx, `SELECT seller_id AS key, max(updated_at) AS updated_at FROM listings
WHERE status = ANY(@public) GROUP BY seller_id`, size)
}

// parts numbers the rows of entries, (key, updated_at), by key from the
// highest and cuts them every size rows. A part's cursor is the lowest key
// of the part before it.
func (s pgListings) parts(ctx context.Context, entries string, size int) ([]SitemapPart, error) {
	rows, err := s.db.Query(ctx, `SELECT (array_agg(key ORDER BY key))[1], max(updated_at) FROM (
    SELECT key, updated_at, row_number() OVER (ORDER BY key DESC) AS n FROM (`+entries+`) e
) r GROUP BY (n - 1) / @size ORDER BY (n - 1) / @size`,
		pgx.StrictNamedArgs{"public": domain.Public, "size": size})
	if err != nil {
		return nil, fmt.Errorf("select sitemap parts: %w", err)
	}
	var (
		parts []SitemapPart
		low   uuid.UUID
		at    time.Time
		prev  uuid.UUID
	)
	if _, err = pgx.ForEachRow(rows, []any{&low, &at}, func() error {
		parts = append(parts, SitemapPart{Cursor: prev, UpdatedAt: at})
		prev = low
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read sitemap parts: %w", err)
	}
	return parts, nil
}

func (s pgListings) SitemapListings(ctx context.Context, before uuid.UUID, size int) ([]SitemapListing, error) {
	rows, err := s.db.Query(ctx, `SELECT title, code, updated_at FROM listings
WHERE status = ANY(@public) AND id < @before ORDER BY id DESC LIMIT @size`,
		pgx.StrictNamedArgs{"public": domain.Public, "before": before, "size": size})
	if err != nil {
		return nil, fmt.Errorf("select sitemap listings: %w", err)
	}
	ls, err := pgx.CollectRows(rows, pgx.RowToStructByPos[SitemapListing])
	if err != nil {
		return nil, fmt.Errorf("read sitemap listings: %w", err)
	}
	return ls, nil
}

func (s pgListings) SitemapSellers(ctx context.Context, before uuid.UUID, size int) ([]SitemapSeller, error) {
	rows, err := s.db.Query(ctx, `SELECT seller_id, max(updated_at) FROM listings
WHERE status = ANY(@public) AND seller_id < @before GROUP BY seller_id ORDER BY seller_id DESC LIMIT @size`,
		pgx.StrictNamedArgs{"public": domain.Public, "before": before, "size": size})
	if err != nil {
		return nil, fmt.Errorf("select sitemap sellers: %w", err)
	}
	ss, err := pgx.CollectRows(rows, pgx.RowToStructByPos[SitemapSeller])
	if err != nil {
		return nil, fmt.Errorf("read sitemap sellers: %w", err)
	}
	return ss, nil
}

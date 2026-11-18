package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/pgvec"
)

// PriceSample measures exact distances on the candidates of one category
// and condition, a set small enough that no vector index is needed. Strict
// named args refuse an unused parameter, so each side's arguments go in
// with its list.
func (s pgVectors) PriceSample(ctx context.Context, f PriceFilter) ([]domain.PriceSample, error) {
	args := pgx.StrictNamedArgs{
		"category": f.CategoryCode, "condition": string(f.Condition), "exclude": f.Exclude,
		"sold": string(domain.StatusSold), "on_sale": []string{string(domain.StatusActive), string(domain.StatusReserved)},
		"sold_since": f.SoldSince, "on_sale_since": f.OnSaleSince, "limit": f.Limit,
	}
	var lists []string
	if f.Text != nil {
		args["tq"], args["tv"], args["tmax"] = pgvec.Format(f.Text), f.TextVersion, 1-f.TextMin
		lists = append(lists, `SELECT t.listing_id AS id, t.embedding <=> @tq::halfvec(1024) AS d
FROM listing_text_vectors AS t JOIN candidates AS c ON c.id = t.listing_id
WHERE t.model_version = @tv AND t.embedding <=> @tq::halfvec(1024) <= @tmax`)
	}
	if f.Photo != nil {
		args["pq"], args["pv"], args["pmax"] = pgvec.Format(f.Photo), f.PhotoVersion, 1-f.PhotoMin
		lists = append(lists, `SELECT p.listing_id AS id, p.embedding <=> @pq::halfvec(1152) AS d
FROM listing_photo_vectors AS p JOIN candidates AS c ON c.id = p.listing_id
WHERE p.model_version = @pv AND p.embedding <=> @pq::halfvec(1152) <= @pmax`)
	}
	if len(lists) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `WITH candidates AS (
SELECT id, price, status = @sold AS sold, published_at, sold_at, lat, lng FROM listings
WHERE category_code = @category AND condition = @condition AND seller_id <> @exclude
  AND ((status = @sold AND sold_at >= @sold_since) OR (status = ANY(@on_sale) AND published_at >= @on_sale_since))),
near AS (SELECT id, min(d) AS d FROM (`+strings.Join(lists, "\nUNION ALL\n")+`) v GROUP BY id)
SELECT c.id, c.price, c.sold,
    CASE WHEN c.sold THEN greatest(1, ceil(extract(epoch FROM c.sold_at - c.published_at) / 86400))::int ELSE 0 END,
    c.lat, c.lng
FROM near AS n JOIN candidates AS c ON c.id = n.id
ORDER BY n.d, c.id LIMIT @limit`, args)
	if err != nil {
		return nil, fmt.Errorf("price sample: %w", err)
	}
	samples, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PriceSample, error) {
		var p domain.PriceSample
		if scanErr := row.Scan(&p.ListingID, &p.Price, &p.Sold, &p.DaysToSell, &p.Point.Lat, &p.Point.Lng); scanErr != nil {
			return p, fmt.Errorf("scan a sample: %w", scanErr)
		}
		return p, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read price sample: %w", err)
	}
	return samples, nil
}

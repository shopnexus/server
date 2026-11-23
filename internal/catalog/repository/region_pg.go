package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

type pgCategories struct{ db *pgxpool.Pool }

func NewCategories(db *pgxpool.Pool) Categories { return pgCategories{db} }

func (s pgCategories) Tree(ctx context.Context, t locale.Tag) ([]domain.Category, error) {
	rows, err := s.db.Query(ctx, `
SELECT c.code, coalesce(c.parent_code, ''), coalesce(n.name, c.display_name),
       coalesce((SELECT json_agg(json_build_object('key', s.key, 'name', coalesce(sn.name, s.display_name)) ORDER BY k.i)
                 FROM unnest(c.spec_keys) WITH ORDINALITY AS k (key, i) JOIN specs s ON s.key = k.key
                 LEFT JOIN spec_names sn ON sn.key = s.key AND sn.locale = @locale), '[]'),
       ARRAY(SELECT s.display_name FROM unnest(c.spec_keys) WITH ORDINALITY AS k (key, i)
             JOIN specs s ON s.key = k.key ORDER BY k.i)
FROM categories c
LEFT JOIN categories p ON p.code = c.parent_code
LEFT JOIN category_names n ON n.code = c.code AND n.locale = @locale
ORDER BY c.parent_code IS NOT NULL, coalesce(p.sort_order, c.sort_order), c.sort_order`, pgx.StrictNamedArgs{"locale": string(t)})
	if err != nil {
		return nil, fmt.Errorf("select categories: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Category])
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	return out, nil
}

func (s pgCategories) Leaf(ctx context.Context, code string) (bool, []domain.SpecNames, error) {
	var specs []domain.SpecNames
	err := s.db.QueryRow(ctx, `
SELECT coalesce((SELECT json_agg(json_build_object('key', s.key, 'names',
                    jsonb_build_object(@vi::text, s.display_name) ||
                    coalesce((SELECT jsonb_object_agg(n.locale, n.name) FROM spec_names n WHERE n.key = s.key), '{}')) ORDER BY k.i)
                 FROM unnest(c.spec_keys) WITH ORDINALITY AS k (key, i) JOIN specs s ON s.key = k.key), '[]')
FROM categories c WHERE c.code = @code AND c.parent_code IS NOT NULL`,
		pgx.StrictNamedArgs{"code": code, "vi": string(locale.Vietnamese)}).Scan(&specs)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("check category: %w", err)
	}
	return true, specs, nil
}

func (s pgCategories) SpecNames(ctx context.Context, t locale.Tag, keys []string) (map[string]string, error) {
	rows, err := s.db.Query(ctx, `SELECT s.key, coalesce(n.name, s.display_name) FROM specs s
LEFT JOIN spec_names n ON n.key = s.key AND n.locale = @locale WHERE s.key = ANY(@keys)`,
		pgx.StrictNamedArgs{"locale": string(t), "keys": keys})
	if err != nil {
		return nil, fmt.Errorf("select spec names: %w", err)
	}
	out := make(map[string]string, len(keys))
	var key, name string
	if _, err = pgx.ForEachRow(rows, []any{&key, &name}, func() error {
		out[key] = name
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read spec names: %w", err)
	}
	return out, nil
}

type pgRegions struct{ db *pgxpool.Pool }

func NewRegions(db *pgxpool.Pool) Regions { return pgRegions{db} }

func (s pgRegions) Provinces(ctx context.Context) ([]domain.Province, error) {
	rows, err := s.db.Query(ctx, `SELECT code, display_name FROM provinces ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("select provinces: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Province])
	if err != nil {
		return nil, fmt.Errorf("read provinces: %w", err)
	}
	return out, nil
}

func (s pgRegions) Communes(ctx context.Context, provinceCode string) ([]domain.Commune, error) {
	rows, err := s.db.Query(ctx, `SELECT code, province_code, display_name, lat, lng FROM communes
WHERE province_code = @province ORDER BY display_name`, pgx.StrictNamedArgs{"province": provinceCode})
	if err != nil {
		return nil, fmt.Errorf("select communes: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Commune])
	if err != nil {
		return nil, fmt.Errorf("read communes: %w", err)
	}
	return out, nil
}

func (s pgRegions) Places(ctx context.Context, codes []string) ([]Place, error) {
	rows, err := s.db.Query(ctx, `SELECT c.code, c.province_code, c.display_name, c.lat, c.lng, p.display_name
FROM communes c JOIN provinces p ON p.code = c.province_code WHERE c.code = ANY(@codes)`, pgx.StrictNamedArgs{"codes": codes})
	if err != nil {
		return nil, fmt.Errorf("select communes: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Place])
	if err != nil {
		return nil, fmt.Errorf("read communes: %w", err)
	}
	return out, nil
}

func (s pgRegions) FindPlaces(ctx context.Context, names []string) (map[string][]domain.Point, error) {
	rows, err := s.db.Query(ctx, `
WITH asked AS (SELECT DISTINCT unnest(@names::text[]) AS place_name)
SELECT avg(c.lat), avg(c.lng), a.place_name FROM asked a
JOIN provinces p ON lower(p.display_name) = lower(a.place_name)
   OR lower(regexp_replace(p.display_name, '^(Thành phố|Tỉnh) ', '')) = lower(a.place_name)
JOIN communes c ON c.province_code = p.code
GROUP BY a.place_name, p.code
UNION ALL
SELECT c.lat, c.lng, a.place_name FROM asked a
JOIN communes c ON lower(c.display_name) = lower(a.place_name)
   OR lower(regexp_replace(c.display_name, '^(Phường|Xã|Đặc khu) ', '')) = lower(a.place_name)`,
		pgx.StrictNamedArgs{"names": names})
	if err != nil {
		return nil, fmt.Errorf("find places: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		domain.Point

		Name string
	}])
	if err != nil {
		return nil, fmt.Errorf("read places: %w", err)
	}
	out := make(map[string][]domain.Point, len(names))
	for _, f := range found {
		out[f.Name] = append(out[f.Name], f.Point)
	}
	return out, nil
}

func (s pgRegions) Nearest(ctx context.Context, p domain.Point) (Place, error) {
	rows, err := s.db.Query(ctx, `SELECT c.code, c.province_code, c.display_name, c.lat, c.lng, p.display_name
FROM communes c JOIN provinces p ON p.code = c.province_code
ORDER BY power(c.lat - @lat, 2) + power((c.lng - @lng) * cos(radians(@lat)), 2), c.code LIMIT 1`,
		pgx.StrictNamedArgs{"lat": p.Lat, "lng": p.Lng})
	if err != nil {
		return Place{}, fmt.Errorf("select nearest commune: %w", err)
	}
	place, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Place])
	if err != nil {
		return Place{}, fmt.Errorf("read nearest commune: %w", err)
	}
	return place, nil
}

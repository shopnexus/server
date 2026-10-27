package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

type pgCategories struct{ db *pgxpool.Pool }

func NewCategories(db *pgxpool.Pool) Categories { return pgCategories{db} }

func (s pgCategories) Tree(ctx context.Context) ([]domain.Category, error) {
	rows, err := s.db.Query(ctx, `
SELECT c.code, coalesce(c.parent_code, ''), c.display_name FROM categories c LEFT JOIN categories p ON p.code = c.parent_code
ORDER BY c.parent_code IS NOT NULL, coalesce(p.sort_order, c.sort_order), c.sort_order`)
	if err != nil {
		return nil, fmt.Errorf("select categories: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Category])
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	return out, nil
}

func (s pgCategories) IsLeaf(ctx context.Context, code string) (bool, error) {
	var leaf bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE code = @code AND parent_code IS NOT NULL)`,
		pgx.StrictNamedArgs{"code": code}).Scan(&leaf)
	if err != nil {
		return false, fmt.Errorf("check category: %w", err)
	}
	return leaf, nil
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


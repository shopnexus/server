package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

func (s pgListings) CountByStatus(ctx context.Context, seller uuid.UUID) (map[domain.Status]int, error) {
	rows, err := s.db.Query(ctx, `SELECT status, count(*) FROM listings WHERE seller_id = @seller GROUP BY status`,
		pgx.StrictNamedArgs{"seller": seller})
	if err != nil {
		return nil, fmt.Errorf("count listings: %w", err)
	}
	defer rows.Close()
	out := map[domain.Status]int{}
	for rows.Next() {
		var (
			st domain.Status
			n  int
		)
		if err = rows.Scan(&st, &n); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		out[st] = n
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read counts: %w", err)
	}
	return out, nil
}

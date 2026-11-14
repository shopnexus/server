package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func (s pgDeals) BuyerOrders(ctx context.Context, buyer uuid.UUID, since time.Time) (domain.BuyerOrders, error) {
	var out domain.BuyerOrders
	if err := s.db.QueryRow(ctx, `SELECT
    count(*) FILTER (WHERE requested_at >= @since),
    count(*) FILTER (WHERE requested_at >= @since AND status = 'failed'),
    count(*) FILTER (WHERE status = ANY(@open))
FROM orders WHERE buyer_id = @buyer`, pgx.StrictNamedArgs{"buyer": buyer, "since": since, "open": domain.OpenStatuses}).Scan(
		&out.Placed, &out.Failed, &out.Open); err != nil {
		return domain.BuyerOrders{}, fmt.Errorf("count buyer orders: %w", err)
	}
	return out, nil
}

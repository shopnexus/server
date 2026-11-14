package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

func (s pgDeals) BuyerOrders(ctx context.Context, buyer uuid.UUID, since time.Time) (BuyerOrders, error) {
	var out BuyerOrders
	if err := s.db.QueryRow(ctx, `SELECT
    count(*) FILTER (WHERE requested_at >= @since),
    count(*) FILTER (WHERE requested_at >= @since AND status = 'failed'),
    count(*) FILTER (WHERE status IN ('pending', 'shipped'))
FROM orders WHERE buyer_id = @buyer`, pgx.StrictNamedArgs{"buyer": buyer, "since": since}).Scan(
		&out.Placed, &out.Failed, &out.Open); err != nil {
		return BuyerOrders{}, fmt.Errorf("count buyer orders: %w", err)
	}
	return out, nil
}

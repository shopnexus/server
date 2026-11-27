package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

func (s pgDeals) BuyerOrders(ctx context.Context, buyer uuid.UUID, since time.Time) (domain.BuyerOrders, error) {
	var out domain.BuyerOrders
	if err := s.db.QueryRow(ctx, `SELECT
    count(*) FILTER (WHERE requested_at >= @since),
    count(*) FILTER (WHERE requested_at >= @since AND status = 'failed' AND shipment_status IS DISTINCT FROM 'lost'),
    count(*) FILTER (WHERE status = ANY(@open))
FROM orders WHERE buyer_id = @buyer`, pgx.StrictNamedArgs{"buyer": buyer, "since": since, "open": domain.OpenStatuses}).Scan(
		&out.Placed, &out.Failed, &out.Open); err != nil {
		return domain.BuyerOrders{}, fmt.Errorf("count buyer orders: %w", err)
	}
	return out, nil
}

func (s pgDeals) AwaitingShipment(ctx context.Context, seller uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE seller_id = @seller AND status = @pending`,
		pgx.StrictNamedArgs{"seller": seller, "pending": domain.OrderPending}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pending sales: %w", err)
	}
	return n, nil
}

func (s pgDeals) CountOrders(ctx context.Context, since time.Time) (map[domain.OrderStatus]int, error) {
	return postgres.Counts[domain.OrderStatus](s.db.Query(ctx, `SELECT status, count(*) FROM orders WHERE requested_at >= @since GROUP BY status`,
		pgx.StrictNamedArgs{"since": since}))
}

func (s pgDeals) CountDeals(ctx context.Context) (map[domain.Status]int, error) {
	return postgres.Counts[domain.Status](s.db.Query(ctx, `SELECT status, count(*) FROM deals GROUP BY status`))
}

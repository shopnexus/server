package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func (s pgDeals) OpenOrders(ctx context.Context, account uuid.UUID) (bool, error) {
	var open bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders
WHERE (buyer_id = @account OR seller_id = @account) AND status IN ('pending', 'shipped'))`,
		pgx.StrictNamedArgs{"account": account}).Scan(&open); err != nil {
		return false, fmt.Errorf("check open orders: %w", err)
	}
	return open, nil
}

func (s pgDeals) ActiveOf(ctx context.Context, account uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, selectDeal+`WHERE (d.buyer_id = @account OR d.seller_id = @account)
AND d.status IN ('negotiating', 'agreed') ORDER BY d.id`, pgx.StrictNamedArgs{"account": account})
}

func (s pgDeals) ScrubRecipients(ctx context.Context, buyer uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE orders SET recipient_name = '', recipient_phone = '', recipient_street = ''
WHERE buyer_id = @buyer`, pgx.StrictNamedArgs{"buyer": buyer}); err != nil {
		return fmt.Errorf("scrub recipients: %w", err)
	}
	return nil
}

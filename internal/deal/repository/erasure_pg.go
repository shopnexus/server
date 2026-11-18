package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/deal/domain"
)

func (s pgDeals) OpenOrders(ctx context.Context, account uuid.UUID) (bool, error) {
	var open bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders
WHERE (buyer_id = @account OR seller_id = @account) AND status = ANY(@open))`,
		pgx.StrictNamedArgs{"account": account, "open": domain.OpenStatuses}).Scan(&open); err != nil {
		return false, fmt.Errorf("check open orders: %w", err)
	}
	return open, nil
}

func (s pgDeals) ActiveOf(ctx context.Context, account uuid.UUID) ([]domain.Deal, error) {
	return s.many(ctx, selectDeal+`WHERE (d.buyer_id = @account OR d.seller_id = @account)
AND d.status IN ('negotiating', 'agreed') ORDER BY d.id`, pgx.StrictNamedArgs{"account": account})
}

func (s pgDeals) ScrubRecipients(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE orders SET recipient_name = '', recipient_phone = '', recipient_street = ''
WHERE buyer_id = @account`, pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("scrub recipients: %w", err)
	}
	// A seller's address copied as a linked shipment's pickup goes too; a
	// carrier's pickup point is the carrier's and stays.
	if _, err := s.db.Exec(ctx, `UPDATE orders
SET pickup_name = NULL, pickup_phone = NULL, pickup_street = NULL, pickup_commune = NULL
WHERE seller_id = @account AND pickup_name IS NOT NULL`, pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("scrub pickups: %w", err)
	}
	return nil
}

func (s pgDeals) OpenShipmentsAt(ctx context.Context, seller uuid.UUID, carrier string) (bool, error) {
	var open bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders
WHERE seller_id = @seller AND carrier = @carrier AND status = 'shipped')`,
		pgx.StrictNamedArgs{"seller": seller, "carrier": carrier}).Scan(&open); err != nil {
		return false, fmt.Errorf("check open shipments: %w", err)
	}
	return open, nil
}

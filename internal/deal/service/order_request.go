package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// RequestCod asks for cash on delivery on the buyer's agreed deal, to the
// buyer's address addressID. The order keeps a copy of the address, so a
// later edit or delete of it leaves the order as it was. Someone else's
// address is accountdomain.ErrAddressNotFound; a buyer at
// domain.MaxOpenOrders open orders is domain.ErrCodLimit. It schedules the
// order's seller timeout first, so it needs Restate.
func (s *Deals) RequestCod(ctx context.Context, caller, id, addressID uuid.UUID) (DealView, error) {
	m, err := s.load(ctx, caller, id, true)
	if err != nil {
		return DealView{}, err
	}
	a, err := s.accounts.Address(ctx, caller, addressID)
	if err != nil {
		return DealView{}, fmt.Errorf("read the address: %w", err)
	}
	recipient := domain.Recipient{Name: a.Name, Phone: a.Phone, Street: a.Street, CommuneCode: a.CommuneCode}
	if err = m.deal.RequestCod(m.by, recipient, m.rules, m.now); err != nil {
		return DealView{}, err
	}
	// The timer goes before the save: one that finds no pending order, as
	// after a refused save, does nothing.
	ref := OrderRef{DealID: id, OrderID: m.deal.Order().ID}
	if _, err = ingress.ServiceSend[OrderRef](s.ingress, s.prefix+orderTimers, "SellerTimeout").
		Send(ctx, ref, restate.WithDelay(s.waits.Seller), restate.WithIdempotencyKey(ref.OrderID.String())); err != nil {
		return DealView{}, fmt.Errorf("schedule the seller's timeout: %w", err)
	}
	return s.save(ctx, caller, m)
}

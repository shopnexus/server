package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/deal/domain"
)

// RequestCod is UC-DEAL-03. The order keeps a copy of the address, so a
// later edit or delete of it leaves the order as it was.
func (s *Deals) RequestCod(ctx context.Context, caller, id, addressID uuid.UUID) (DealView, error) {
	m, err := s.load(ctx, caller, id, true)
	if err != nil {
		return DealView{}, err
	}
	// E2: someone else's address is accountdomain.ErrAddressNotFound.
	a, err := s.accounts.Address(ctx, caller, addressID)
	if err != nil {
		return DealView{}, fmt.Errorf("read the address: %w", err)
	}
	recipient := domain.Recipient{
		Name: a.Name, Phone: a.Phone, Street: a.Street, CommuneCode: a.CommuneCode, Kind: domain.AddressKind(a.Kind),
	}
	// 2. The pending order; E4 and E5 are the domain's refusals, E3 the
	// store's, which counts open orders in the save (BR-DEAL-06).
	if err = m.deal.RequestCod(m.by, recipient, m.rules, m.now); err != nil {
		return DealView{}, err
	}
	// The seller's timeout (UC-DEAL-06 A1) goes before the save: a timer
	// that finds no pending order, as after a refused save, does nothing.
	ref := OrderRef{DealID: id, OrderID: m.deal.Order().ID}
	if _, err = ingress.ServiceSend[OrderRef](s.ingress, s.prefix+orderTimers, "SellerTimeout").
		Send(ctx, ref, restate.WithDelay(s.waits.Seller), restate.WithIdempotencyKey(ref.OrderID.String())); err != nil {
		return DealView{}, fmt.Errorf("schedule the seller's timeout: %w", err)
	}
	return s.save(ctx, caller, m)
}

package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// RequestCod asks for cash on delivery on the buyer's agreed deal, to the
// buyer's address addressID. The order keeps a copy of the address, so a
// later edit or delete of it leaves the order as it was. Someone else's
// address is accountdomain.ErrAddressNotFound; a buyer at
// domain.MaxOpenOrders open orders is domain.ErrCodLimit.
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
	return s.save(ctx, caller, m)
}

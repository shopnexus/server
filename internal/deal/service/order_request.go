package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
)

// RequestCod asks for cash on delivery to r on the buyer's agreed deal. A
// buyer at domain.MaxOpenOrders open orders is domain.ErrCodLimit.
func (s *Deals) RequestCod(ctx context.Context, caller, id uuid.UUID, r RecipientInput) (DealView, error) {
	m, err := s.load(ctx, caller, id, true)
	if err != nil {
		return DealView{}, err
	}
	_, err = s.catalog.Place(ctx, r.CommuneCode)
	if err != nil && !errors.Is(err, catalogdomain.ErrNotFound) {
		return DealView{}, fmt.Errorf("read the commune: %w", err)
	}
	recipient, err := domain.NewRecipient(r.Name, r.Phone, r.Street, r.CommuneCode, err == nil)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.RequestCod(m.by, recipient, m.rules, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

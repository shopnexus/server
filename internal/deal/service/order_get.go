package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
)

func (s *Deals) OrderFor(ctx context.Context, viewer, id uuid.UUID) (OrderView, uuid.UUID, error) {
	r, err := s.d.Deals.Order(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && viewer != r.BuyerID && viewer != r.SellerID) {
		return OrderView{}, uuid.Nil(), domain.ErrOrderNotFound
	}
	if err != nil {
		return OrderView{}, uuid.Nil(), fmt.Errorf("read order: %w", err)
	}
	places, err := s.d.Catalog.Places(ctx, []string{r.Order.Recipient.CommuneCode})
	if err != nil {
		return OrderView{}, uuid.Nil(), fmt.Errorf("read recipient commune: %w", err)
	}
	by := domain.Seller
	if viewer == r.BuyerID {
		by = domain.Buyer
	}
	return orderView(r.Order, by, places[r.Order.Recipient.CommuneCode], s.d.Waits), r.DealID, nil
}

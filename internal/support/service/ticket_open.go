package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/support/domain"
)

// Open checks the request, then runs TicketOpen once per account and key,
// and reads the ticket it opened or found.
func (s *Support) Open(ctx context.Context, account, key uuid.UUID, in OpenInput) (TicketView, bool, error) {
	if _, err := domain.NewTicket(in.Kind, in.CarrierName, in.Body, in.OrderID); err != nil {
		return TicketView{}, false, err
	}
	res, err := s.open.Call(ctx, account.String()+"/"+key.String(), OpenFlowInput{Account: account, Key: key, Input: in})
	if err != nil {
		return TicketView{}, false, err
	}
	t, err := s.tickets.Get(ctx, res.TicketID)
	if err != nil {
		return TicketView{}, false, fmt.Errorf("read the ticket: %w", err)
	}
	v, err := s.view(ctx, t)
	return v, res.Existed, err
}

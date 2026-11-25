package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/support/domain"
)

// Open checks the request; a key the account opened a ticket with already
// answers that ticket (NFR-IDEM-01), otherwise it runs TicketOpen and
// reads the ticket it opened or found.
func (s *Support) Open(ctx context.Context, account, key uuid.UUID, in OpenInput) (TicketView, bool, error) {
	if _, err := domain.NewTicket(in.Kind, in.CarrierName, in.Text, len(in.Photos), in.OrderID); err != nil {
		return TicketView{}, false, err
	}
	var existed bool
	t, err := s.tickets.ByKey(ctx, account, key)
	if errors.Is(err, domain.ErrNotFound) {
		res, callErr := s.open.Call(ctx, "", OpenFlowInput{Account: account, Key: key, Input: in})
		if callErr != nil {
			return TicketView{}, false, callErr
		}
		existed = res.Existed
		t, err = s.tickets.Get(ctx, res.TicketID)
	}
	if err != nil {
		return TicketView{}, false, fmt.Errorf("read the ticket: %w", err)
	}
	v, err := s.view(ctx, t)
	return v, existed, err
}

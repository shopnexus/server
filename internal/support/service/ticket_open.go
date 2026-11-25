package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
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
	t, err := s.d.Tickets.ByKey(ctx, account, key)
	if errors.Is(err, domain.ErrNotFound) {
		res, callErr := s.open.Call(ctx, OpenFlowInput{Account: account, Key: key, Input: in})
		if callErr != nil {
			return TicketView{}, false, callErr
		}
		existed = res.Existed
		t, err = s.d.Tickets.Get(ctx, res.TicketID)
		// A racing retry answered a ticket its first run then withdrew.
		if errors.Is(err, domain.ErrNotFound) {
			return TicketView{}, false, apperr.ErrTryAgain
		}
	}
	if err != nil {
		return TicketView{}, false, fmt.Errorf("read the ticket: %w", err)
	}
	v, err := s.view(ctx, t)
	return v, existed, err
}

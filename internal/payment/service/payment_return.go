package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/provider/payment"
)

// Return is the payment the gateway sent the payer back from; Found is
// false for a query whose signature does not check or that names no
// payment.
type Return struct {
	Found   bool
	Payment View
}

// Returned is UC-LST-03, step 9: the payer's browser comes back. The
// redirect alone moves nothing: a pending payment it calls paid makes the
// server ask the gateway (querydr), and only that answer records it.
func (s *Payments) Returned(ctx context.Context, q url.Values) (Return, error) {
	if s.d.Gateway == nil {
		return Return{}, nil
	}
	r, err := s.d.Gateway.Verify(q)
	if err != nil {
		return Return{}, nil //nolint:nilerr // a bad signature is a page that says so
	}
	id, ok := payment.IDOf(r.Ref)
	if !ok {
		return Return{}, nil
	}
	p, err := s.d.Store.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return Return{}, nil
	}
	if err != nil {
		return Return{}, fmt.Errorf("read payment %s: %w", id, err)
	}
	if p.Status() == domain.StatusPending && r.Paid {
		now := s.d.Now()
		got, qerr := s.d.Gateway.Query(ctx, lookup(p.State()), now)
		switch {
		case qerr == nil && got.Paid:
			if _, err = s.report(ctx, id, result(got), now, uuid.NewV7(), uuid.NewV7()); err != nil && !errors.Is(err, domain.ErrAmountMismatch) {
				return Return{}, err
			}
			if p, err = s.d.Store.Get(ctx, id); err != nil {
				return Return{}, fmt.Errorf("read payment %s: %w", id, err)
			}
		case qerr != nil && !errors.Is(qerr, payment.ErrNotFound):
			s.d.Log.WarnContext(ctx, "ask the gateway about a returned payment", "payment", id, "err", qerr)
		default: // not paid yet; the IPN or the expiry records it
		}
	}
	return Return{Found: true, Payment: p.State()}, nil
}

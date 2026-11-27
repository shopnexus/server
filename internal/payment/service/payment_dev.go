package service

import (
	"context"
	"fmt"
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/provider/payment"
)

// pending reads a payment the fake still waits on; domain.ErrNotFound for
// another, one past its expiry as VNPay's page refuses it, or when the
// gateway is not the fake.
func (s *Payments) pending(ctx context.Context, id uuid.UUID) (payment.Fake, domain.Payment, error) {
	fake, ok := s.d.Gateway.(payment.Fake)
	if !ok {
		return payment.Fake{}, domain.Payment{}, domain.ErrNotFound
	}
	p, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return payment.Fake{}, domain.Payment{}, fmt.Errorf("read payment %s: %w", id, err)
	}
	if p.Status() != domain.StatusPending || !s.d.Now().Before(p.State().ExpiresAt) {
		return payment.Fake{}, domain.Payment{}, domain.ErrNotFound
	}
	return fake, p, nil
}

func (s *Payments) DevPayment(ctx context.Context, id uuid.UUID) (View, error) {
	_, p, err := s.pending(ctx, id)
	if err != nil {
		return View{}, err
	}
	return p.State(), nil
}

// DevPay makes the IPN call VNPay would, in process and through the same
// signature check, then hands back the query for the return page.
func (s *Payments) DevPay(ctx context.Context, id uuid.UUID, paid bool) (url.Values, error) {
	fake, p, err := s.pending(ctx, id)
	if err != nil {
		return nil, err
	}
	q := fake.Callback(id, p.Amount(), paid, s.d.Now())
	if a := s.Notify(ctx, q); a != answerRecorded && a != answerDone {
		return nil, fmt.Errorf("fake IPN of payment %s answered %s %s", id, a.RspCode, a.Message)
	}
	return q, nil
}

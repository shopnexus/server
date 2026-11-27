package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/provider/payment"
)

// Open is UC-LST-03, step 6: the gateway's page for the price, and the
// expiry scheduled before the row exists, so no payment goes unexpired.
// An expiry that finds no row, after a lost race, does nothing.
func (s *Payments) Open(ctx context.Context, in OpenInput) (View, error) {
	if s.d.Gateway == nil {
		return View{}, domain.ErrUnavailable
	}
	now := s.d.Now()
	id := uuid.NewV7()
	payURL, err := s.d.Gateway.PayURL(payment.Order{
		ID: id, Amount: in.Amount, Info: info, IP: in.IP, English: in.English, CreatedAt: now, ExpiresAt: now.Add(domain.ExpireAfter),
	})
	if err != nil {
		return View{}, fmt.Errorf("build the payment page: %w", err)
	}
	p := domain.NewPayment(id, in.Account, in.Product, in.ProductID, in.Amount, string(s.d.Gateway.Gateway()), payURL, now)
	if _, err = ingress.ServiceSend[uuid.UUID](s.d.Ingress, s.d.Prefix+flows, "Expire").
		Send(ctx, id, restate.WithDelay(domain.ExpireAfter), restate.WithIdempotencyKey("expire-"+id.String())); err != nil {
		return View{}, fmt.Errorf("schedule the payment's expiry: %w", err)
	}
	stored, err := s.d.Store.Insert(ctx, p)
	if err != nil {
		return View{}, fmt.Errorf("store the payment: %w", err)
	}
	return stored.State(), nil
}

// Refund sends the refund flow; one already sent for the payment is not
// sent again.
func (s *Payments) Refund(ctx context.Context, id uuid.UUID) error {
	if _, err := ingress.ServiceSend[uuid.UUID](s.d.Ingress, s.d.Prefix+flows, "Refund").
		Send(ctx, id, restate.WithIdempotencyKey("refund-"+id.String())); err != nil {
		return fmt.Errorf("send the refund of payment %s: %w", id, err)
	}
	return nil
}

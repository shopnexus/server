package service

import (
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/payment"
)

// flows is the PaymentFlows service's name without the prefix.
const flows = "PaymentFlows"

// Flows is a payment's wait that outlives a request: its expiry. It reads
// the payment when it runs and does nothing when the payment moved on.
type Flows struct{ Payments *Payments }

func (f Flows) ServiceName() string { return f.Payments.d.Prefix + flows }

// pendingRead is a payment as a step journals it; Found is false for one
// never stored.
type pendingRead struct {
	Found   bool           `json:"found"`
	Payment domain.Payment `json:"payment"`
}

// queried is the gateway's answer to querydr as a step journals it.
type queried struct {
	Known  bool          `json:"known"`
	Result domain.Result `json:"result"`
}

// Expire is UC-LST-03 A3: a payment still pending ExpireAfter after it
// opened. The gateway is asked first (querydr), so a payment whose IPN was
// lost is recorded paid; any other answer expires it with payment.expired.
func (f Flows) Expire(ctx restate.Context, id uuid.UUID) error {
	s := f.Payments
	at, err := restatex.Now(ctx, s.d.Now)
	if err != nil {
		return err
	}
	read, err := restate.Run(ctx, func(rc restate.RunContext) (pendingRead, error) {
		p, getErr := s.d.Store.Get(rc, id)
		if errors.Is(getErr, domain.ErrNotFound) {
			return pendingRead{}, nil // a lost race at Open left no row
		}
		if getErr != nil {
			return pendingRead{}, fmt.Errorf("read payment %s: %w", id, getErr)
		}
		return pendingRead{Found: true, Payment: p}, nil
	})
	if err != nil {
		return fmt.Errorf("read the expiring payment: %w", err)
	}
	if !read.Found || read.Payment.Status() != domain.StatusPending || s.d.Gateway == nil {
		return nil
	}
	q, err := restate.Run(ctx, func(rc restate.RunContext) (queried, error) {
		r, queryErr := s.d.Gateway.Query(rc, lookup(read.Payment.State()), at)
		switch {
		case queryErr == nil:
			return queried{Known: true, Result: result(r)}, nil
		case errors.Is(queryErr, payment.ErrNotFound) || errors.Is(queryErr, payment.ErrRefused):
			return queried{}, nil
		default:
			return queried{}, fmt.Errorf("querydr: %w", queryErr)
		}
	})
	if err != nil {
		return fmt.Errorf("ask the gateway: %w", err)
	}
	writeID, eventID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		if q.Known && q.Result.Paid {
			_, reportErr := s.report(rc, id, q.Result, at, writeID, eventID)
			if !errors.Is(reportErr, domain.ErrAmountMismatch) {
				return reportErr
			}
			s.d.Log.WarnContext(rc, "querydr of another amount", "payment", id, "amount", q.Result.Amount)
		}
		changeErr := s.change(rc, id, writeID, eventID, at, func(p *domain.Payment) (events.Kind, error) {
			if p.Expire(at) {
				return events.PaymentExpired, nil
			}
			return "", nil
		})
		return changeErr
	}); err != nil {
		return fmt.Errorf("expire payment %s: %w", id, err)
	}
	return nil
}

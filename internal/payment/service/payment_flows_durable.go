package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/payment"
)

// flows is the PaymentFlows service's name without the prefix.
const flows = "PaymentFlows"

// Flows is a payment's waits and calls that outlive a request: its expiry
// and its refund. Each reads the payment when it runs and does nothing
// when the payment moved on.
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
		_, changeErr := s.change(rc, id, writeID, eventID, at, func(p *domain.Payment) (events.Kind, error) {
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

// Refund is UC-LST-03 A4 and A5: the full amount back to the payer. It
// marks the payment refunding, has the gateway refund it, then writes
// payment.refunded. A refund the gateway refuses stops the flow with the
// payment left refunding for staff; any other fault is retried.
func (f Flows) Refund(ctx restate.Context, id uuid.UUID) error {
	s := f.Payments
	at, err := restatex.Now(ctx, s.d.Now)
	if err != nil {
		return err
	}
	writeID := restatex.NewID(ctx, at)
	p, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Payment, error) {
		got, changeErr := s.change(rc, id, writeID, uuid.Nil(), at, func(p *domain.Payment) (events.Kind, error) {
			started, startErr := p.StartRefund()
			if startErr != nil || !started {
				return "", startErr
			}
			return kindNone, nil
		})
		return got, refundFault(changeErr)
	})
	if err != nil {
		return fmt.Errorf("start the refund of payment %s: %w", id, err)
	}
	return f.giveBack(ctx, p, at)
}

// giveBack is Refund once the payment is refunding.
func (f Flows) giveBack(ctx restate.Context, p domain.Payment, at time.Time) error {
	s := f.Payments
	if p.Status() == domain.StatusRefunded || s.d.Gateway == nil {
		return nil
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		refundErr := s.d.Gateway.Refund(rc, lookup(p.State()), at)
		if errors.Is(refundErr, payment.ErrRefused) {
			s.d.Log.ErrorContext(rc, "the gateway refused a refund", "payment", p.ID(), "err", refundErr)
			return restate.ToTerminalError(refundErr)
		}
		if refundErr != nil {
			return fmt.Errorf("refund at the gateway: %w", refundErr)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("refund payment %s: %w", p.ID(), err)
	}
	writeID, eventID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		_, changeErr := s.change(rc, p.ID(), writeID, eventID, at, func(p *domain.Payment) (events.Kind, error) {
			if p.Refunded(at) {
				return events.PaymentRefunded, nil
			}
			return "", nil
		})
		return changeErr
	}); err != nil {
		return fmt.Errorf("record the refund of payment %s: %w", p.ID(), err)
	}
	return nil
}

// refundFault stops a refund of a payment that is not there or not paid,
// which no retry changes.
func refundFault(err error) error {
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrNotRefundable) {
		return restate.ToTerminalError(fmt.Errorf("refund: %w", err)) //nolint:wrapcheck // err is wrapped already
	}
	return err
}

// Package service is module payment's use cases: a payment opened for a
// sale, the gateway's reports of it, its expiry and its refund.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/payment/repository"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/provider/payment"
)

// View is a payment as the modules that sell read it.
type View = domain.State

// OpenInput is a payment to open: amount for the product's row ProductID,
// paid by Account from IP. Info is the line the gateway shows, in ASCII.
type OpenInput struct {
	Account   uuid.UUID
	Product   domain.Product
	ProductID uuid.UUID
	Amount    int64
	Info      string
	IP        string
	English   bool
}

// NotifyAnswer is what the IPN answers the gateway.
type NotifyAnswer struct {
	RspCode string `json:"RspCode"` //nolint:tagliatelle // VNPay's field names.
	Message string `json:"Message"` //nolint:tagliatelle // VNPay's field names.
}

// Service is module payment, as the modules that sell call it.
type Service interface {
	// Available reports whether this server takes payments; with
	// PAYMENT_VNPAY=off it takes none and Open is domain.ErrUnavailable.
	Available() bool
	// Open opens a payment and schedules its expiry ExpireAfter later. A
	// payment for the same product row already open is returned as it is.
	Open(ctx context.Context, in OpenInput) (View, error)
	// Get reads a payment; domain.ErrNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (View, error)
	// Many reads the payments among ids, keyed by ID.
	Many(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]View, error)
	// Refund gives a paid payment back in full, in a flow of its own that
	// writes payment.refunded once the gateway took it.
	Refund(ctx context.Context, id uuid.UUID) error
	// Notify records the gateway's server-to-server report (IPN).
	Notify(ctx context.Context, q url.Values) NotifyAnswer
	// Returned reads the payment the gateway sent the payer back from.
	Returned(ctx context.Context, q url.Values) (Return, error)
	// DevPayment reads a pending payment for the fake's page; Fake only.
	DevPayment(ctx context.Context, id uuid.UUID) (View, error)
	// DevPay pays or cancels a pending payment as the fake's payer would,
	// and returns the query the gateway sends the payer back with.
	DevPay(ctx context.Context, id uuid.UUID, paid bool) (url.Values, error)
}

// Deps is what the service needs. Gateway is nil when the server takes no
// payments.
type Deps struct {
	Store   repository.Store
	Gateway payment.Client
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Log     *slog.Logger
}

// Payments implements Service.
type Payments struct{ d Deps }

var _ Service = (*Payments)(nil)

func New(d Deps) *Payments { return &Payments{d: d} }

func (s *Payments) Available() bool { return s.d.Gateway != nil }

func (s *Payments) Get(ctx context.Context, id uuid.UUID) (View, error) {
	p, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("read payment %s: %w", id, err)
	}
	return p.State(), nil
}

func (s *Payments) Many(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]View, error) {
	ps, err := s.d.Store.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read payments: %w", err)
	}
	out := make(map[uuid.UUID]View, len(ps))
	for id := range ps {
		p := ps[id]
		out[id] = p.State()
	}
	return out, nil
}

// saveTries bounds the reads again after a save that lost a race.
const saveTries = 5

// change reads payment id, lets f change it, and saves it under writeID
// with an event of the kind f returns; f returning no kind saves nothing.
// A save that lost a race reads again. The event's ID is eventID.
func (s *Payments) change(ctx context.Context, id, writeID, eventID uuid.UUID, at time.Time,
	f func(p *domain.Payment) (events.Kind, error),
) (domain.Payment, error) {
	for range saveTries {
		p, err := s.d.Store.Get(ctx, id)
		if err != nil {
			return domain.Payment{}, fmt.Errorf("read payment %s: %w", id, err)
		}
		kind, err := f(&p)
		if err != nil || kind == "" {
			return p, err
		}
		var evs []events.Event
		if kind != kindNone {
			evs = append(evs, events.Event{ID: eventID, Kind: kind, SubjectID: id, OccurredAt: at})
		}
		err = s.d.Store.Save(ctx, &p, writeID, evs...)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return domain.Payment{}, fmt.Errorf("save payment %s: %w", id, err)
		}
		return p, nil
	}
	return domain.Payment{}, fmt.Errorf("save payment %s: %w", id, repository.ErrConflict)
}

// kindNone is a change f saves without an event.
const kindNone events.Kind = "-"

// report records r on payment id: payment.succeeded or payment.failed when
// it changed the payment.
func (s *Payments) report(ctx context.Context, id uuid.UUID, r domain.Result, at time.Time, writeID, eventID uuid.UUID) (domain.Outcome, error) {
	outcome := domain.Unchanged
	_, err := s.change(ctx, id, writeID, eventID, at, func(p *domain.Payment) (events.Kind, error) {
		var err error
		outcome, err = p.Report(r, at)
		switch {
		case err != nil:
			return "", err
		case outcome == domain.Paid:
			return events.PaymentSucceeded, nil
		case outcome == domain.Failed:
			return events.PaymentFailed, nil
		default:
			return "", nil
		}
	})
	return outcome, err
}

// result is a gateway's result as the domain reads it.
func result(r payment.Result) domain.Result {
	return domain.Result{
		Amount: r.Amount, Paid: r.Paid, ResultCode: r.ResponseCode, TransactionNo: r.TransactionNo, BankCode: r.BankCode,
	}
}

// lookup names p to the gateway.
func lookup(p domain.State) payment.Lookup {
	return payment.Lookup{ID: p.ID, Amount: p.Amount, Info: info, TransactionNo: p.GatewayRef, CreatedAt: p.CreatedAt}
}

// info is the line every payment shows at the gateway, ASCII as VNPay
// requires; a request names the payment by its reference.
const info = "ShopNexus"

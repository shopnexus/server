// Package worker holds module boost's Restate handlers for the payment
// events it follows.
package worker

import (
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// Worker follows payments: Mount subscribes each handler to its
// payment.* kind. The event's subject is the payment.
type Worker struct {
	Prefix string
	Boosts service.Service
	Now    func() time.Time
}

func (w Worker) ServiceName() string { return w.Prefix + "BoostWorker" }

// run calls f in a step with the handling's journaled time and IDs, so a
// retried step writes the same rows.
func (w Worker) run(ctx restate.Context, e events.Event, f func(rc restate.RunContext, at time.Time, writeID, eventID uuid.UUID) error) error {
	at, err := restatex.Now(ctx, w.Now)
	if err != nil {
		return err
	}
	writeID, eventID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return f(rc, at, writeID, eventID) }); err != nil {
		return fmt.Errorf("%s of payment %s: %w", e.Kind, e.SubjectID, err)
	}
	return nil
}

// Succeeded starts the boost the payment paid for, or refunds it.
func (w Worker) Succeeded(ctx restate.Context, e events.Event) error {
	return w.run(ctx, e, func(rc restate.RunContext, at time.Time, writeID, eventID uuid.UUID) error {
		return w.Boosts.PaymentPaid(rc, e.SubjectID, at, writeID, eventID)
	})
}

// Failed closes the boost of a failed payment.
func (w Worker) Failed(ctx restate.Context, e events.Event) error {
	return w.run(ctx, e, func(rc restate.RunContext, at time.Time, writeID, eventID uuid.UUID) error {
		return w.Boosts.PaymentClosed(rc, e.SubjectID, domain.StatusFailed, at, writeID, eventID)
	})
}

// Expired closes the boost of a payment never made.
func (w Worker) Expired(ctx restate.Context, e events.Event) error {
	return w.run(ctx, e, func(rc restate.RunContext, at time.Time, writeID, eventID uuid.UUID) error {
		return w.Boosts.PaymentClosed(rc, e.SubjectID, domain.StatusExpired, at, writeID, eventID)
	})
}

// Refunded records the refund of a boost's payment.
func (w Worker) Refunded(ctx restate.Context, e events.Event) error {
	return w.run(ctx, e, func(rc restate.RunContext, at time.Time, writeID, eventID uuid.UUID) error {
		return w.Boosts.PaymentRefunded(rc, e.SubjectID, at, writeID, eventID)
	})
}

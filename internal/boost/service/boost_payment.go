package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/platform/events"
)

// PaymentPaid is UC-LST-03, step 10: the payment landed. A boost awaiting
// it moves its listing up and starts, its first tick sent before the save
// so a crash between them loses no tick. A5: a listing no longer on sale
// leaves the package unstarted and refunds it. A4: a boost that closed or
// was replaced first, or none, is refunded.
func (s *Boosts) PaymentPaid(ctx context.Context, payment uuid.UUID, at time.Time, writeID, eventID uuid.UUID) error {
	refund := false
	b, err := s.change(ctx, payment, writeID, eventID, at, func(b *domain.Boost) (events.Kind, error) {
		refund = false
		switch b.Status() {
		case domain.StatusAwaiting:
			bumped, err := s.d.Catalog.Bump(ctx, b.SellerID(), b.ListingID(), at)
			if err != nil {
				return "", fmt.Errorf("move the listing up: %w", err)
			}
			if bumped && b.Start(at) {
				if err = s.sendTick(ctx, b.ID(), *b.NextTick(), at); err != nil {
					return "", err
				}
				return events.BoostStarted, nil
			}
		case domain.StatusRefunding:
			refund = true
			return "", nil
		case domain.StatusActive, domain.StatusEnded, domain.StatusRefunded:
			return "", nil
		case domain.StatusFailed, domain.StatusExpired, domain.StatusCancelled:
		default:
		}
		refund = b.Refuse(at)
		return kindNone, nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		refund = true // a payment whose boost was never stored
	} else if err != nil {
		return err
	}
	if !refund || (b.ID() != uuid.Nil() && b.Status() != domain.StatusRefunding) {
		return nil
	}
	if err = s.d.Payments.Refund(ctx, payment); err != nil {
		return fmt.Errorf("refund the boost's payment: %w", err)
	}
	return nil
}

// PaymentClosed is UC-LST-03 A3 and E8: the payment expired or failed, so
// the boost awaiting it closes with boost.expired or boost.failed.
func (s *Boosts) PaymentClosed(ctx context.Context, payment uuid.UUID, status domain.Status, at time.Time, writeID, eventID uuid.UUID) error {
	kind := events.BoostFailed
	if status == domain.StatusExpired {
		kind = events.BoostExpired
	}
	_, err := s.change(ctx, payment, writeID, eventID, at, func(b *domain.Boost) (events.Kind, error) {
		if b.Close(status, at) {
			return kind, nil
		}
		return "", nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// PaymentRefunded is UC-LST-03 A4 and A5, at their end: the gateway gave
// the money back, and boost.refunded tells the seller.
func (s *Boosts) PaymentRefunded(ctx context.Context, payment uuid.UUID, at time.Time, writeID, eventID uuid.UUID) error {
	_, err := s.change(ctx, payment, writeID, eventID, at, func(b *domain.Boost) (events.Kind, error) {
		if b.Refunded(at) {
			return events.BoostRefunded, nil
		}
		return "", nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// sendTick schedules the tick of a running boost at next; a send again
// for the same tick is deduped.
func (s *Boosts) sendTick(ctx context.Context, id uuid.UUID, next, now time.Time) error {
	if _, err := ingress.ServiceSend[TickRef](s.d.Ingress, s.d.Prefix+timers, "Tick").
		Send(ctx, TickRef{BoostID: id, At: next}, restate.WithDelay(max(next.Sub(now), 0)),
			restate.WithIdempotencyKey(fmt.Sprintf("%s/%d", id, next.Unix()))); err != nil {
		return fmt.Errorf("schedule the boost's tick: %w", err)
	}
	return nil
}

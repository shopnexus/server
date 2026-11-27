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

// PaymentPaid is UC-LST-03, step 10: the payment landed, and paid money
// always starts its package, its days counted from the payment (BR-LST-07).
// The listing moves up when on sale; A5: one not on sale starts unmoved,
// and its ticks skip until it is. The first tick is sent before the save,
// so a crash between them loses none. A4: a boost whose payment expired or
// failed first starts too, logged, unless another package is live there.
func (s *Boosts) PaymentPaid(ctx context.Context, payment uuid.UUID, at time.Time, writeID, eventID uuid.UUID) error {
	p, err := s.d.Payments.Get(ctx, payment)
	if err != nil {
		return fmt.Errorf("read the paid payment: %w", err)
	}
	from := at
	if p.PaidAt != nil {
		from = *p.PaidAt
	}
	err = s.change(ctx, payment, writeID, eventID, at, func(b *domain.Boost) (events.Kind, error) {
		switch b.Status() {
		case domain.StatusAwaiting:
		case domain.StatusFailed, domain.StatusExpired:
			// A4
			s.d.Log.ErrorContext(ctx, "a payment landed after its boost closed", "boost", b.ID(), "payment", payment)
			live, liveErr := s.d.Store.Live(ctx, []uuid.UUID{b.ListingID()})
			if liveErr != nil {
				return "", fmt.Errorf("read the listing's boost: %w", liveErr)
			}
			if _, taken := live[b.ListingID()]; taken {
				s.d.Log.ErrorContext(ctx, "another package is live on the listing, so the paid boost stays closed",
					"boost", b.ID(), "payment", payment)
				return "", nil
			}
		case domain.StatusActive, domain.StatusEnded:
			return "", nil
		default:
			return "", nil
		}
		var bumpedAt *time.Time
		bumped, bumpErr := s.d.Catalog.Bump(ctx, b.SellerID(), b.ListingID(), at)
		if bumpErr != nil {
			return "", fmt.Errorf("move the listing up: %w", bumpErr)
		}
		if bumped {
			bumpedAt = &at
		}
		if !b.Start(from, bumpedAt) {
			return "", nil
		}
		if tickErr := s.sendTick(ctx, b.ID(), *b.NextTick(), at); tickErr != nil {
			return "", tickErr
		}
		return events.BoostStarted, nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		s.d.Log.ErrorContext(ctx, "a payment landed for no boost", "payment", payment)
		return nil
	}
	return err
}

// PaymentClosed is UC-LST-03 A3 and E8: the payment expired or failed, so
// the boost awaiting it closes with boost.expired or boost.failed.
func (s *Boosts) PaymentClosed(ctx context.Context, payment uuid.UUID, status domain.Status, at time.Time, writeID, eventID uuid.UUID) error {
	kind := events.BoostFailed
	if status == domain.StatusExpired {
		kind = events.BoostExpired
	}
	err := s.change(ctx, payment, writeID, eventID, at, func(b *domain.Boost) (events.Kind, error) {
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

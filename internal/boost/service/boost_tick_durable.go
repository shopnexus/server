package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/repository"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// timers is the BoostTimers service's name without the prefix.
const timers = "BoostTimers"

// TickRef names a boost's tick: the boost and the time it is due. Restate
// keeps it for the whole wait, so its JSON stays the same across releases.
type TickRef struct {
	BoostID uuid.UUID `json:"boost_id"`
	At      time.Time `json:"at"`
}

// Timers moves running packages on. A tick that finds its boost moved on
// does nothing, so a duplicate changes nothing.
type Timers struct{ Boosts *Boosts }

func (t Timers) ServiceName() string { return t.Boosts.d.Prefix + timers }

// Tick is UC-LST-03, steps 11 and 12: a day of a running package passed.
// It moves the listing up while it is on sale (A6: a day it is not makes
// no move, and adds no day), then waits for the next; the last tick ends
// the package with boost.ended.
func (t Timers) Tick(ctx restate.Context, ref TickRef) error {
	at, err := restatex.Now(ctx, t.Boosts.d.Now)
	if err != nil {
		return err
	}
	writeID, eventID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	done, err := restate.Run(ctx, func(rc restate.RunContext) (ticked, error) {
		return t.Boosts.tick(rc, ref, at, writeID, eventID)
	})
	if err != nil {
		return fmt.Errorf("tick boost %s: %w", ref.BoostID, err)
	}
	if done.Next == nil {
		return nil
	}
	restate.ServiceSend(ctx, t.ServiceName(), "Tick").Send(TickRef{BoostID: ref.BoostID, At: *done.Next},
		restate.WithDelay(max(done.Next.Sub(at), 0)))
	return nil
}

// ticked is what a tick's step did: the next tick's time, nil when there
// is none.
type ticked struct {
	Next *time.Time `json:"next,omitempty"`
}

// tick is Tick's step.
func (s *Boosts) tick(ctx context.Context, ref TickRef, at time.Time, writeID, eventID uuid.UUID) (ticked, error) {
	for range saveTries {
		b, err := s.d.Store.Get(ctx, ref.BoostID)
		if errors.Is(err, domain.ErrNotFound) || (err == nil && !b.Due(ref.At)) {
			return ticked{}, nil
		}
		if err != nil {
			return ticked{}, fmt.Errorf("read the boost: %w", err)
		}
		// 12.
		if b.Last(ref.At) {
			b.End(at)
			err = s.d.Store.Save(ctx, &b, writeID, events.Event{ID: eventID, Kind: events.BoostEnded, SubjectID: b.ID(), OccurredAt: at})
			if errors.Is(err, repository.ErrConflict) {
				continue
			}
			if err != nil {
				return ticked{}, fmt.Errorf("end the boost: %w", err)
			}
			return ticked{}, nil
		}
		// 11.
		bumped, err := s.d.Catalog.Bump(ctx, b.SellerID(), b.ListingID(), at)
		if err != nil {
			return ticked{}, fmt.Errorf("move the listing up: %w", err)
		}
		next := b.Ticked(ref.At, at, bumped)
		if err = s.d.Store.Save(ctx, &b, writeID); errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return ticked{}, fmt.Errorf("save the tick: %w", err)
		}
		return ticked{Next: &next}, nil
	}
	return ticked{}, fmt.Errorf("tick boost %s: %w", ref.BoostID, repository.ErrConflict)
}

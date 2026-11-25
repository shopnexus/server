package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/repository"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// loadStep reads a read in a step; Found is false when its row is gone.
func (d Deps) loadStep(ctx restate.Context, id uuid.UUID) (loadedRead, error) {
	loaded, err := restate.Run(ctx, func(rc restate.RunContext) (loadedRead, error) {
		r, stepErr := d.Reads.Get(rc, id)
		if errors.Is(stepErr, domain.ErrNotFound) {
			return loadedRead{}, nil
		}
		if stepErr != nil {
			return loadedRead{}, fmt.Errorf("get read: %w", stepErr)
		}
		return loadedRead{Read: r, Found: true}, nil
	})
	if err != nil {
		return loadedRead{}, fmt.Errorf("load read: %w", err)
	}
	return loaded, nil
}

// readRun is the read one Run works on, as the journal keeps it, and the
// steps that write it.
type readRun struct {
	d        Deps
	ctx      restate.Context
	id       uuid.UUID
	r        domain.ListingRead
	deadline time.Time
	// release runs before an end is saved: a video read consumes its video.
	release func(at time.Time) error
}

// loadRun loads the read in a step; false when it is gone or no longer
// reading.
func (d Deps) loadRun(ctx restate.Context, id uuid.UUID) (*readRun, bool, error) {
	loaded, err := d.loadStep(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !loaded.Found || loaded.Read.Status() != domain.StatusReading {
		return nil, false, nil
	}
	return &readRun{d: d, ctx: ctx, id: id, r: loaded.Read, deadline: loaded.Read.CreatedAt().Add(domain.Deadline)}, true, nil
}

// save writes the read under a new write ID and keeps the saved copy, so
// the next save carries its write ID. gone is a row that went meanwhile.
func (x *readRun) save(at time.Time) (gone bool, err error) {
	w := restatex.NewID(x.ctx, at)
	r := x.r
	out, err := restate.Run(x.ctx, func(rc restate.RunContext) (savedRead, error) {
		c := r // Save sets the write ID on its copy; r outside stays as journaled
		lost, stepErr := restatex.Outcome(x.d.Reads.Save(rc, &c, w), repository.ErrConflict)
		if stepErr != nil {
			return savedRead{}, fmt.Errorf("save read: %w", stepErr)
		}
		return savedRead{Read: c, Gone: lost}, nil
	})
	if err != nil {
		return false, fmt.Errorf("save read: %w", err)
	}
	x.r = out.Read
	return out.Gone, nil
}

// end releases what the read holds, saves it and tells its watchers.
func (x *readRun) end(at time.Time) error {
	if x.release != nil {
		if err := x.release(at); err != nil {
			return err
		}
	}
	_, err := x.save(at)
	x.d.relayEnd(x.ctx, x.id)
	return err
}

// fail ends the read failed.
func (x *readRun) fail(at time.Time, f domain.Failure, reason string) error {
	// A1: the read fails and the seller fills the form by hand.
	if err := x.r.Fail(f, reason); err != nil {
		return fmt.Errorf("fail read: %w", err)
	}
	return x.end(at)
}

// finish ends the read done.
func (x *readRun) finish(at time.Time) error {
	if err := x.r.Finish(); err != nil {
		return fmt.Errorf("finish read: %w", err)
	}
	return x.end(at)
}

// tick reads the journaled clock and, past the deadline, fails the read
// with unavailable; done says Run returns err.
func (x *readRun) tick() (at time.Time, done bool, err error) {
	at, err = restatex.Now(x.ctx, x.d.Now)
	if err != nil {
		return at, true, err
	}
	if !at.Before(x.deadline) {
		return at, true, x.fail(at, domain.FailUnavailable, pastDeadline)
	}
	return at, false, nil
}

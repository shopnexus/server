package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/discovery/domain"
)

// SweepService is the Restate service that sweeps one watch, and
// VectorSweepService the one that sweeps the text vectors, without the
// prefix; worker.SearchWatchSweep and worker.SearchVectorSweep serve them.
const (
	SweepService       = "SearchWatchSweep"
	VectorSweepService = "SearchVectorSweep"
)

// Sweeper starts a new watch's chain of sweeps, and the day's sweep of the
// text vectors.
type Sweeper interface {
	Schedule(ctx context.Context, watch uuid.UUID) error
	SweepVectors(ctx context.Context, day string) error
}

type restateSweeper struct {
	ingress       *ingress.Client
	name, vectors string
}

func NewSweeper(in *ingress.Client, prefix string) Sweeper {
	return restateSweeper{ingress: in, name: prefix + SweepService, vectors: prefix + VectorSweepService}
}

// SweepVectors sends the sweep of the text vectors keyed by day, so the
// processes that start it on one day run it once.
func (s restateSweeper) SweepVectors(ctx context.Context, day string) error {
	if _, err := ingress.ServiceSend[string](s.ingress, s.vectors, "Run").
		Send(ctx, day, restate.WithIdempotencyKey("vectors-"+day)); err != nil {
		return fmt.Errorf("start the vector sweep: %w", err)
	}
	return nil
}

// Schedule sends the watch's first sweep, domain.SweepEvery from now, keyed
// by the watch, so a retried POST starts one chain. It runs before the row
// exists, as an upload's expiry does: no watch is left without a chain, and
// a sweep that finds no row ends at once.
func (s restateSweeper) Schedule(ctx context.Context, watch uuid.UUID) error {
	if _, err := ingress.ServiceSend[uuid.UUID](s.ingress, s.name, "Run").
		Send(ctx, watch, restate.WithDelay(domain.SweepEvery), restate.WithIdempotencyKey(watch.String())); err != nil {
		return fmt.Errorf("schedule the sweep: %w", err)
	}
	return nil
}

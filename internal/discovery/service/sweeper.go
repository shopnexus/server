package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/discovery/domain"
)

// SweepService is the Restate service that sweeps one watch, without the
// prefix; worker.SearchWatchSweep serves it.
const SweepService = "SearchWatchSweep"

// Sweeper starts a new watch's chain of sweeps.
type Sweeper interface {
	Schedule(ctx context.Context, watch uuid.UUID) error
}

type restateSweeper struct {
	ingress *ingress.Client
	name    string
}

func NewSweeper(in *ingress.Client, prefix string) Sweeper {
	return restateSweeper{ingress: in, name: prefix + SweepService}
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

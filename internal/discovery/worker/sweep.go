package worker

import (
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/discovery/service"
)

// SearchWatchSweep sweeps one watch per invocation and sends the next: at
// once after a full batch, Every later otherwise. A watch gone ends the
// chain. Each invocation journals one step and one send.
type SearchWatchSweep struct {
	Prefix    string
	Discovery service.Service
	// Every is domain.SweepEvery in production.
	Every time.Duration
}

func (w SearchWatchSweep) ServiceName() string { return w.Prefix + service.SweepService }

func (w SearchWatchSweep) Run(ctx restate.Context, watch uuid.UUID) error {
	r, err := restate.Run(ctx, func(rc restate.RunContext) (service.Swept, error) {
		return w.Discovery.Sweep(rc, watch)
	})
	if err != nil {
		return fmt.Errorf("sweep %s: %w", watch, err)
	}
	if !r.Found {
		return nil
	}
	next := restate.ServiceSend(ctx, w.ServiceName(), "Run")
	if r.Full {
		next.Send(watch)
	} else {
		next.Send(watch, restate.WithDelay(w.Every))
	}
	return nil
}

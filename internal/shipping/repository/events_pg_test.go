package repository_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/repository"
)

// A webhook and a poll of one event, dated minutes apart and racing, keep
// it once.
func TestEventsAddRacingKeepsOne(t *testing.T) {
	t.Parallel()
	events := repository.NewEvents(pgtest.New(t))
	at := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	var kept atomic.Int32
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			ev := carrier.Event{At: at.Add(time.Duration(i) * time.Minute), Kind: carrier.KindStep, Step: carrier.StepPicked, CarrierStatus: "3"}
			added, err := events.Add(context.Background(), "ghtk", "S1", ev)
			if err != nil {
				t.Error(err)
			}
			if added {
				kept.Add(1)
			}
		})
	}
	wg.Wait()
	if kept.Load() != 1 {
		t.Fatalf("kept %d", kept.Load())
	}
}

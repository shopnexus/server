package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/discovery/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// calls is a discovery service that records the listing calls.
type calls struct {
	service.Service

	mu      *sync.Mutex
	arrived *[]uuid.UUID
	moved   *[]uuid.UUID
}

func (c calls) ListingArrived(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.arrived = append(*c.arrived, id)
	return nil
}

func (c calls) PriceMoved(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.moved = append(*c.moved, id)
	return nil
}

// A first publication arrives and weighs the saves; a republication and an
// edit only weigh them.
func TestListingWatch(t *testing.T) {
	t.Parallel()
	var arrived, moved []uuid.UUID
	mu := &sync.Mutex{}
	w := worker.ListingWatch{Prefix: restatetest.Prefix(), Discovery: calls{mu: mu, arrived: &arrived, moved: &moved}}
	client := restatetest.Start(t, restate.Reflect(w))
	first, again := uuid.NewV7(), uuid.NewV7()
	for _, c := range []struct {
		handler string
		e       events.Event
	}{
		{"Published", events.Event{ID: uuid.NewV7(), Kind: events.ListingPublished, SubjectID: first, Payload: map[string]any{"first": true}, OccurredAt: time.Now()}},
		{"Updated", events.Event{ID: uuid.NewV7(), Kind: events.ListingUpdated, SubjectID: again, OccurredAt: time.Now()}},
		{"Published", events.Event{ID: uuid.NewV7(), Kind: events.ListingPublished, SubjectID: again, OccurredAt: time.Now()}},
	} {
		if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), c.handler).Request(context.Background(), c.e); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(arrived) != 1 || arrived[0] != first || len(moved) != 3 {
		t.Fatalf("arrived %v, moved %v", arrived, moved)
	}
}

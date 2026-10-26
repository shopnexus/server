package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/deal/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// recorder is a deal service that records the listings ListingChanged
// was called with.
type recorder struct {
	service.Service

	mu   *sync.Mutex
	seen *[]uuid.UUID
}

func (r recorder) ListingChanged(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.seen = append(*r.seen, id)
	return nil
}

func TestChangedCallsListingChanged(t *testing.T) {
	t.Parallel()
	var seen []uuid.UUID
	mu := &sync.Mutex{}
	w := worker.ListingWatch{Prefix: restatetest.Prefix(), Deals: recorder{mu: mu, seen: &seen}}
	client := restatetest.Start(t, restate.Reflect(w))
	e := events.Event{ID: uuid.NewV7(), Kind: events.ListingUpdated, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
	changed := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), "Changed")
	for range 2 {
		if _, err := changed.Request(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != e.SubjectID || seen[1] != e.SubjectID {
		t.Fatalf("%v", seen)
	}
}

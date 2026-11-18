package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/discovery/worker"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// recorder is a discovery service that records the accounts
// AccountDeleted was called with.
type recorder struct {
	service.Service

	mu   *sync.Mutex
	seen *[]uuid.UUID
}

func (r recorder) AccountDeleted(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.seen = append(*r.seen, id)
	return nil
}

func TestDeletedCallsAccountDeleted(t *testing.T) {
	t.Parallel()
	var seen []uuid.UUID
	mu := &sync.Mutex{}
	w := worker.AccountWatch{Prefix: restatetest.Prefix(), Discovery: recorder{mu: mu, seen: &seen}}
	client := restatetest.Start(t, restate.Reflect(w))
	e := events.Event{ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
	for range 2 {
		if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), "Deleted").Request(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != e.SubjectID || seen[1] != e.SubjectID {
		t.Fatalf("%v", seen)
	}
}

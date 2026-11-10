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

// accountRecorder is a deal service that records AccountDeleted and
// ScrubRecipients.
type accountRecorder struct {
	service.Service

	mu       *sync.Mutex
	deleted  *[]uuid.UUID
	scrubbed *[]uuid.UUID
}

func (r accountRecorder) AccountDeleted(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.deleted = append(*r.deleted, id)
	return nil
}

func (r accountRecorder) ScrubRecipients(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.scrubbed = append(*r.scrubbed, id)
	return nil
}

func TestAccountWatch(t *testing.T) {
	t.Parallel()
	var deleted, scrubbed []uuid.UUID
	mu := &sync.Mutex{}
	w := worker.AccountWatch{Prefix: restatetest.Prefix(), Deals: accountRecorder{mu: mu, deleted: &deleted, scrubbed: &scrubbed}}
	client := restatetest.Start(t, restate.Reflect(w))
	e := events.Event{ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
	for range 2 {
		if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), "Deleted").Request(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingress.Service[uuid.UUID, restate.Void](client, w.ServiceName(), "Scrub").Request(context.Background(), e.SubjectID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 2 || deleted[0] != e.SubjectID || len(scrubbed) != 1 || scrubbed[0] != e.SubjectID {
		t.Fatalf("deleted %v, scrubbed %v", deleted, scrubbed)
	}
}

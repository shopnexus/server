package worker_test

import (
	"context"
	"slices"
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

// poster is a deal service that records what PostMoves was called with.
type poster struct {
	service.Service

	mu    *sync.Mutex
	deal  *uuid.UUID
	moves *[]uuid.UUID
}

func (p poster) PostMoves(_ context.Context, deal uuid.UUID, ids []uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.deal, *p.moves = deal, ids
	return nil
}

// Moved hands the deal and the move IDs of deal.moved to PostMoves; a
// payload entry that is not an ID is left out.
func TestMovedPostsTheMoves(t *testing.T) {
	t.Parallel()
	var (
		deal  uuid.UUID
		moves []uuid.UUID
	)
	mu := &sync.Mutex{}
	w := worker.ChatPost{Prefix: restatetest.Prefix(), Deals: poster{mu: mu, deal: &deal, moves: &moves}}
	client := restatetest.Start(t, restate.Reflect(w))
	want := []uuid.UUID{uuid.NewV7(), uuid.NewV7()}
	e := events.Event{
		ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: uuid.NewV7(), OccurredAt: time.Now(),
		Payload: map[string]any{"moves": []any{want[0].String(), "not an id", want[1].String()}},
	}
	if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), "Moved").Request(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if deal != e.SubjectID || !slices.Equal(moves, want) {
		t.Fatalf("%s %v", deal, moves)
	}
}

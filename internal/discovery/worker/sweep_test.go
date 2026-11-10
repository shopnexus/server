package worker_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/discovery/worker"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// sweeps answers Full twice, then Found, then not Found, counting the runs.
type sweeps struct {
	service.Service

	mu   *sync.Mutex
	runs *int
}

func (s sweeps) Sweep(context.Context, uuid.UUID) (service.Swept, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.runs++
	switch *s.runs {
	case 1, 2:
		return service.Swept{Found: true, Full: true}, nil
	case 3:
		return service.Swept{Found: true}, nil
	default:
		return service.Swept{}, nil
	}
}

// A full batch sweeps again at once, a partial one after Every, and a watch
// gone ends the chain.
func TestSweepChain(t *testing.T) {
	t.Parallel()
	runs, mu := 0, &sync.Mutex{}
	w := worker.SearchWatchSweep{Prefix: restatetest.Prefix(), Discovery: sweeps{mu: mu, runs: &runs}, Every: 2 * time.Second}
	client := restatetest.Start(t, restate.Reflect(w))
	start := time.Now()
	if _, err := ingress.Service[uuid.UUID, restate.Void](client, w.ServiceName(), "Run").Request(context.Background(), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 20*time.Second, func() error {
		mu.Lock()
		defer mu.Unlock()
		if runs < 4 {
			return fmt.Errorf("%d runs", runs)
		}
		return nil
	})
	if time.Since(start) < 2*time.Second {
		t.Fatal("the partial batch waited Every")
	}
	time.Sleep(3 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if runs != 4 {
		t.Fatalf("the chain ended: %d runs", runs)
	}
}

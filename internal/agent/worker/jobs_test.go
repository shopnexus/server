package worker_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/agent/worker"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// counted is an agent service that counts its jobs' runs; its reconcile fails.
type counted struct {
	service.Service

	mu   *sync.Mutex
	runs map[string]int
}

func (c counted) count(job string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs[job]++
	return nil
}

func (c counted) SweepTurns(context.Context) error { return c.count(worker.JobTurns) }
func (c counted) SweepReads(context.Context) error { return c.count(worker.JobReads) }
func (c counted) Reconcile(context.Context) error {
	_ = c.count(worker.JobGateway)
	return errors.New("bifrost is down")
}

func (c counted) seen() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.runs)
}

func TestJobKeyNamesThePeriod(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 27, 13, 4, 10, 0, time.UTC)
	same := worker.JobKey(worker.JobTurns, "every", at.Add(40*time.Second), time.Minute)
	if got := worker.JobKey(worker.JobTurns, "every", at, time.Minute); got != same {
		t.Fatalf("one minute, two keys: %s, %s", got, same)
	}
	if worker.JobKey(worker.JobTurns, "every", at.Add(time.Minute), time.Minute) == same {
		t.Fatal("the next minute has the same key")
	}
	if worker.JobKey(worker.JobGateway, "miss", at, time.Minute) == worker.JobKey(worker.JobGateway, "every", at, time.Minute) {
		t.Fatal("a missed key's reconcile shares the scheduled one's key")
	}
}

// Sends with one key run the job once; a failed job is not retried; an
// unknown job fails for good.
func TestJobsRunOncePerKey(t *testing.T) {
	t.Parallel()
	agent := counted{mu: &sync.Mutex{}, runs: map[string]int{}}
	w := worker.Jobs{Prefix: restatetest.Prefix(), Agent: agent, Log: slog.New(slog.DiscardHandler)}
	client := restatetest.Start(t, restate.Reflect(w))
	ctx := context.Background()
	at := time.Now()
	for _, job := range []string{worker.JobTurns, worker.JobReads, worker.JobGateway} {
		for range 3 {
			if _, err := ingress.ServiceSend[string](client, w.ServiceName(), "Run").
				Send(ctx, job, restate.WithIdempotencyKey(worker.JobKey(job, "every", at, time.Hour))); err != nil {
				t.Fatal(err)
			}
		}
	}
	want := map[string]int{worker.JobTurns: 1, worker.JobReads: 1, worker.JobGateway: 1}
	testenv.Eventually(t, 10*time.Second, func() error {
		if got := agent.seen(); fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("runs %v", got)
		}
		return nil
	})
	time.Sleep(2 * time.Second)
	if got := agent.seen(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ran again: %v", got)
	}
	_, err := ingress.Service[string, restate.Void](client, w.ServiceName(), "Run").Request(ctx, "nope")
	if err == nil {
		t.Fatal("an unknown job ran")
	}
}

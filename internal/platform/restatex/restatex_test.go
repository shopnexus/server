package restatex_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/restatetest"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// counter counts how many times its step body really ran.
type counter struct {
	prefix string
	runs   *atomic.Int32
}

func (c counter) ServiceName() string { return c.prefix + "Counter" }

func (c counter) Bump(ctx restate.Context, label string) (string, error) {
	n, err := restate.Run(ctx, func(restate.RunContext) (int32, error) { return c.runs.Add(1), nil })
	if err != nil {
		return "", err
	}
	return label + "-" + strconv.Itoa(int(n)), nil
}

func TestServeRegisterAndCall(t *testing.T) {
	runs := &atomic.Int32{}
	svc := counter{prefix: restatetest.Prefix(), runs: runs}
	client := restatetest.Start(t, restate.Reflect(svc))
	bump := ingress.Service[string, string](client, svc.ServiceName(), "Bump")
	ctx := context.Background()

	first, err := bump.Request(ctx, "a", restate.WithIdempotencyKey("key-1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := bump.Request(ctx, "a", restate.WithIdempotencyKey("key-1"))
	if err != nil || again != first {
		t.Fatalf("same key: %q %v, first %q", again, err, first)
	}
	if runs.Load() != 1 {
		t.Fatalf("the step ran %d times for one key", runs.Load())
	}
	if _, err := bump.Request(ctx, "b", restate.WithIdempotencyKey("key-2")); err != nil || runs.Load() != 2 {
		t.Fatalf("new key: %v, runs %d", err, runs.Load())
	}
}

func TestHealthy(t *testing.T) {
	_, admin := restatetest.URLs(t)
	if err := restatex.Healthy(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	if err := restatex.Healthy(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("a closed port reported healthy")
	}
}

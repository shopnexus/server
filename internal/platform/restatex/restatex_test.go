package restatex_test

import (
	"context"
	"net/http"
	"path/filepath"
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
	if err := restatex.Healthy(context.Background(), restatetest.Config(t).AdminURL); err != nil {
		t.Fatal(err)
	}
	if err := restatex.Healthy(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("a closed port reported healthy")
	}
}

// The endpoint runs only requests the Restate server signed with the key
// whose public half it holds.
func TestServeRejectsUnsignedRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.pem")
	key, err := restatex.EnsureIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := restatex.EnsureIdentity(path); err != nil || again != key {
		t.Fatalf("a second call made another key: %v", err)
	}
	ep, err := restatex.Serve("127.0.0.1:0", key, restate.Reflect(counter{prefix: "t", runs: &atomic.Int32{}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ep.Close(context.Background()) })
	res, err := http.Get(ep.URL() + "/discover")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned discovery: %d", res.StatusCode)
	}
}

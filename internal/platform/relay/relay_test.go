package relay_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/relay"
)

// next is the sub's next message, failing after 5 s.
func next(t *testing.T, s *relay.Sub) json.RawMessage {
	t.Helper()
	select {
	case m := <-s.C():
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message within 5s")
		return nil
	}
}

// A message reaches the watchers of its key only; the first listen sends
// a nil message; Close ends every watch.
func TestRelay(t *testing.T) {
	t.Parallel()
	r := relay.New(pgtest.New(t), slog.New(slog.DiscardHandler))
	key, otherKey := uuid.NewV7(), uuid.NewV7()
	sub, other := r.Subscribe(key), r.Subscribe(otherKey)
	defer other.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)
	if m := next(t, sub); m != nil {
		t.Fatalf("first %s, want nil on listening", m)
	}
	_ = next(t, other)
	if err := r.Publish(ctx, key, json.RawMessage(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if m := next(t, sub); string(m) != `{"n":1}` {
		t.Fatalf("%s", m)
	}
	select {
	case m := <-other.C():
		t.Fatalf("another key heard %s", m)
	case <-time.After(200 * time.Millisecond):
	}
	if err := r.Publish(ctx, key, json.RawMessage(`"`+strings.Repeat("a", relay.MaxMessage)+`"`)); err == nil {
		t.Fatal("an oversized message went out")
	}
	sub.Close()
	r.Close()
	select {
	case <-other.Closed():
	case <-time.After(time.Second):
		t.Fatal("Close did not end the watch")
	}
}

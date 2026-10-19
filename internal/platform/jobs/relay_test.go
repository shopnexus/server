package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shopnexus/server2/internal/platform/natstest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

var discard = slog.New(slog.DiscardHandler)

// startRelay runs r until the test ends.
func startRelay(t *testing.T, r *Relay) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { r.Run(ctx) })
	t.Cleanup(func() { cancel(); wg.Wait() })
	return cancel
}

func eventually(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func streamMsgs(t *testing.T, js jetstream.JetStream) uint64 {
	t.Helper()
	s, err := js.Stream(context.Background(), "CMD")
	if err != nil {
		return 0
	}
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}

func pending(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox WHERE published_at IS NULL").Scan(&n)
	return n
}

func setup(t *testing.T) (*pgxpool.Pool, jetstream.JetStream) {
	t.Helper()
	pool := pgtest.New(t)
	_, js := natstest.Start(t)
	if err := EnsureStreams(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	return pool, js
}

func TestRelayPublishesInOrder(t *testing.T) {
	pool, js := setup(t)
	for i := 1; i <= 3; i++ {
		Enqueue(context.Background(), pool, ping{N: i}, Deadline(time.Now().Add(time.Hour)))
	}
	startRelay(t, NewRelay(pool, js, discard))
	eventually(t, 5*time.Second, func() bool { return pending(t, pool) == 0 })

	s, _ := js.Stream(context.Background(), "CMD")
	for seq := uint64(1); seq <= 3; seq++ {
		m, err := s.GetMsg(context.Background(), seq)
		if err != nil {
			t.Fatal(err)
		}
		// jsonb normalises spacing, so compare decoded values.
		if n := decodeN(t, m.Data); n != int(seq) {
			t.Fatalf("seq %d holds n=%d", seq, n)
		}
		if _, err := uuid.Parse(m.Header.Get("Nats-Msg-Id")); err != nil {
			t.Fatalf("Nats-Msg-Id %q", m.Header.Get("Nats-Msg-Id"))
		}
		if m.Header.Get("Deadline") == "" {
			t.Fatal("no Deadline header")
		}
	}
}

func TestRelayCrashAfterPublishDoesNotDuplicate(t *testing.T) {
	pool, js := setup(t)
	Enqueue(context.Background(), pool, ping{N: 1})
	r := NewRelay(pool, js, discard)
	var calls atomic.Int32
	r.afterPublish = func(uuid.UUID) error {
		if calls.Add(1) == 1 {
			return errors.New("simulated crash before published_at")
		}
		return nil
	}
	startRelay(t, r)
	eventually(t, 5*time.Second, func() bool { return pending(t, pool) == 0 })

	if calls.Load() < 2 {
		t.Fatalf("publish attempts %d", calls.Load())
	}
	if n := streamMsgs(t, js); n != 1 {
		t.Fatalf("stream holds %d messages, want 1", n)
	}
}

func TestOnlyOneRelayPublishes(t *testing.T) {
	pool, js := setup(t)
	first, second := NewRelay(pool, js, discard), NewRelay(pool, js, discard)
	var firstCount, secondCount atomic.Int32
	first.afterPublish = func(uuid.UUID) error { firstCount.Add(1); return nil }
	second.afterPublish = func(uuid.UUID) error { secondCount.Add(1); return nil }

	stopFirst := startRelay(t, first)
	eventually(t, 5*time.Second, first.IsLeader)
	startRelay(t, second)
	for i := range 5 {
		Enqueue(context.Background(), pool, ping{N: i})
	}
	eventually(t, 5*time.Second, func() bool { return pending(t, pool) == 0 })
	if firstCount.Load() != 5 || secondCount.Load() != 0 {
		t.Fatalf("first %d, second %d", firstCount.Load(), secondCount.Load())
	}

	stopFirst()
	eventually(t, 5*time.Second, second.IsLeader)
	Enqueue(context.Background(), pool, ping{N: 9})
	eventually(t, 5*time.Second, func() bool { return pending(t, pool) == 0 })
	if secondCount.Load() != 1 {
		t.Fatalf("second published %d after taking over", secondCount.Load())
	}
}

func TestRelayWakesOnNotify(t *testing.T) {
	pool, js := setup(t)
	r := NewRelay(pool, js, discard)
	r.poll = 10 * time.Second
	startRelay(t, r)
	eventually(t, 5*time.Second, r.IsLeader)
	time.Sleep(100 * time.Millisecond) // let it settle into waiting

	start := time.Now()
	Enqueue(context.Background(), pool, ping{N: 1})
	eventually(t, 2*time.Second, func() bool { return pending(t, pool) == 0 })
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("published after %v; NOTIFY did not wake the relay", d)
	}
}

func TestRelayRetriesUntilStreamExists(t *testing.T) {
	pool := pgtest.New(t)
	_, js := natstest.Start(t) // no streams yet: publishes fail
	Enqueue(context.Background(), pool, ping{N: 1})
	Enqueue(context.Background(), pool, ping{N: 2})
	startRelay(t, NewRelay(pool, js, discard))

	time.Sleep(300 * time.Millisecond)
	if pending(t, pool) != 2 {
		t.Fatal("rows marked published without a stream")
	}
	if err := EnsureStreams(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool { return pending(t, pool) == 0 })
	s, _ := js.Stream(context.Background(), "CMD")
	m, _ := s.GetMsg(context.Background(), 1)
	if decodeN(t, m.Data) != 1 {
		t.Fatalf("first message %s; order lost", m.Data)
	}
}

func decodeN(t *testing.T, data []byte) int {
	t.Helper()
	var p ping
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("payload %s: %v", data, err)
	}
	return p.N
}

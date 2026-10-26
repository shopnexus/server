package events

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// counter is a subscriber that counts how often its handler ran per event.
type counter struct {
	prefix string
	mu     *sync.Mutex
	runs   map[uuid.UUID]int
}

func (c counter) ServiceName() string { return c.prefix + "Counter" }

func (c counter) Handle(ctx restate.Context, e Event) error {
	return restate.RunVoid(ctx, func(restate.RunContext) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.runs[e.ID]++
		return nil
	})
}

func (c counter) count(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs[id]
}

// A round that sent an event and died before marking it sends it again
// under the same key, and the subscriber still runs once.
func TestResendAfterACrashDeliversOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	c := counter{prefix: restatetest.Prefix(), mu: &sync.Mutex{}, runs: map[uuid.UUID]int{}}
	in := restatetest.Start(t, restate.Reflect(c))
	bus := NewBus(db, in, time.Now, slog.New(slog.DiscardHandler))
	bus.Subscribe(AccountRenamed, c.ServiceName(), "Handle")
	e := Event{ID: uuid.NewV7(), Kind: AccountRenamed, SubjectID: uuid.NewV7(), Data: map[string]any{}, At: time.Now().UTC().Truncate(time.Microsecond)}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Insert(ctx, tx, e) }); err != nil {
		t.Fatal(err)
	}
	sub := bus.subs[AccountRenamed][0]
	if _, err := ingress.ServiceSend[Event](in, sub.Service, sub.Handler).
		Send(ctx, e, restate.WithIdempotencyKey(sub.key(e))); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); c.count(e.ID) == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(time.Second) // a second run would land by now
	if n := c.count(e.ID); n != 1 {
		t.Fatalf("handler ran %d times", n)
	}
}

// A Restate that accepts the connection and never answers ends the round
// after sendTimeout instead of holding the transaction open.
func TestSendTimesOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	bus := NewBus(db, ingress.NewClient("http://"+ln.Addr().String()), time.Now, slog.New(slog.DiscardHandler))
	bus.Subscribe(AccountRenamed, "Silent", "Handle")
	e := Event{ID: uuid.NewV7(), Kind: AccountRenamed, SubjectID: uuid.NewV7(), At: time.Now()}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Insert(ctx, tx, e) }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := bus.dispatch(ctx); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("no error from a silent Restate")
		}
	case <-time.After(sendTimeout + 5*time.Second):
		t.Fatal("dispatch hangs on a silent Restate")
	}
}

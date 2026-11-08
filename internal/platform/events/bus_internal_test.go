package events

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// A round that sent an event and died before marking it sends it again
// under the same key, and the subscriber still runs once.
func TestResendAfterACrashDeliversOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	c := NewInbox(p, "Counter")
	in := restatetest.Ingress(t)
	bus := NewBus(db, in, p, time.Now, slog.New(slog.DiscardHandler))
	bus.Subscribe(AccountUpdated, c.ServiceName(), "Handle")
	restatetest.Start(t, bus.Definition(), restate.Reflect(c))
	e := Event{ID: uuid.NewV7(), Kind: AccountUpdated, SubjectID: uuid.NewV7(), Payload: map[string]any{}, OccurredAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Insert(ctx, tx, e) }); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.ServiceSend[Event](in, bus.service(), deliverHandler).
		Send(ctx, e, restate.WithIdempotencyKey(e.ID.String())); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 10*time.Second, func() error {
		if c.Count(e.ID) == 0 {
			return errors.New("the handler has not run")
		}
		return nil
	})
	time.Sleep(time.Second) // a second run would land by now
	if n := c.Count(e.ID); n != 1 {
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
	bus := NewBus(db, ingress.NewClient("http://"+ln.Addr().String()), "silent_", time.Now, slog.New(slog.DiscardHandler))
	bus.Subscribe(AccountUpdated, "Silent", "Handle")
	e := Event{ID: uuid.NewV7(), Kind: AccountUpdated, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
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

// When the ingress refuses the event itself (4xx), the event waits before
// the next try, longer each time, and other events go on meanwhile.
func TestRefusedEventBacksOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	// Nobody serves this bus's Events service, so the ingress answers 404.
	bus := NewBus(db, restatetest.Ingress(t), restatetest.Prefix(), time.Now, slog.New(slog.DiscardHandler))
	bus.Subscribe(AccountUpdated, "Anyone", "Handle")
	e := Event{ID: uuid.NewV7(), Kind: AccountUpdated, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Insert(ctx, tx, e) }); err != nil {
		t.Fatal(err)
	}
	state := func() (attempts int, waits bool) {
		if err := db.QueryRow(ctx, `SELECT attempts, next_attempt_at > now() FROM events WHERE id = @id`,
			pgx.StrictNamedArgs{"id": e.ID}).Scan(&attempts, &waits); err != nil {
			t.Fatal(err)
		}
		return attempts, waits
	}
	for range 2 { // the second round finds the event waiting and skips it
		if _, err := bus.dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if attempts, waits := state(); attempts != 1 || !waits {
		t.Fatalf("after a refusal: %d attempts, waiting %v", attempts, waits)
	}
	if _, err := db.Exec(ctx, `UPDATE events SET next_attempt_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if attempts, waits := state(); attempts != 2 || !waits {
		t.Fatalf("after a second refusal: %d attempts, waiting %v", attempts, waits)
	}
}

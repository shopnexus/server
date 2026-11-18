package events_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

var discard = slog.New(slog.DiscardHandler)

// newBus is a bus on prefix p whose Events service is served, with the
// subscribers given, by an endpoint of the test stack.
func newBus(t *testing.T, db *pgxpool.Pool, p string, subscribers ...events.Inbox) *events.Bus {
	t.Helper()
	bus := events.NewBus(db, restatetest.Ingress(t), p, time.Now, discard)
	defs := make([]restate.ServiceDefinition, 0, 1+len(subscribers))
	defs = append(defs, bus.Definition())
	for _, i := range subscribers {
		defs = append(defs, restate.Reflect(i))
	}
	restatetest.Start(t, defs...)
	return bus
}

// run starts b until the test ends.
func run(t *testing.T, b *events.Bus) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = b.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func publish(t *testing.T, db *pgxpool.Pool, kind events.Kind) events.Event {
	t.Helper()
	e := events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: uuid.NewV7(), OccurredAt: time.Now()}
	if err := insert(t, db, e); err != nil {
		t.Fatal(err)
	}
	return e
}

func dispatched(t *testing.T, db *pgxpool.Pool, id uuid.UUID) bool {
	t.Helper()
	var at *time.Time
	if err := db.QueryRow(context.Background(), `SELECT dispatched_at FROM events WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at != nil
}

func TestEverySubscriberGetsTheEventOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	a, b := events.NewInbox(p, "A"), events.NewInbox(p, "B")
	bus := newBus(t, db, p, a, b)
	bus.Subscribe(events.AccountSuspended, a.ServiceName(), "Handle")
	bus.Subscribe(events.AccountSuspended, b.ServiceName(), "Handle")
	run(t, bus)
	e := publish(t, db, events.AccountSuspended)
	testenv.Eventually(t, 10*time.Second, func() error {
		if na, nb, d := a.Count(e.ID), b.Count(e.ID), dispatched(t, db, e.ID); na != 1 || nb != 1 || !d {
			return fmt.Errorf("delivered %d and %d times, dispatched %t", na, nb, d)
		}
		return nil
	})
	time.Sleep(1500 * time.Millisecond) // one more poll round
	if a.Count(e.ID) != 1 || b.Count(e.ID) != 1 {
		t.Fatalf("delivered %d and %d times", a.Count(e.ID), b.Count(e.ID))
	}
}

func TestTwoBusesDeliverEachEventOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	a := events.NewInbox(p, "A")
	first := newBus(t, db, p, a)
	second := events.NewBus(db, restatetest.Ingress(t), p, time.Now, discard)
	for _, bus := range []*events.Bus{first, second} {
		bus.Subscribe(events.AccountUpdated, a.ServiceName(), "Handle")
		run(t, bus)
	}
	sent := make([]events.Event, 0, 50)
	for range 50 {
		sent = append(sent, publish(t, db, events.AccountUpdated))
	}
	testenv.Eventually(t, 10*time.Second, func() error {
		for i, e := range sent {
			if a.Count(e.ID) == 0 {
				return fmt.Errorf("event %d of %d not delivered", i+1, len(sent))
			}
		}
		return nil
	})
	for _, e := range sent {
		if n := a.Count(e.ID); n != 1 {
			t.Fatalf("%s delivered %d times", e.ID, n)
		}
	}
}

func TestEventWithoutSubscriberIsMarked(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	run(t, events.NewBus(db, restatetest.Ingress(t), restatetest.Prefix(), time.Now, discard))
	e := publish(t, db, events.AccountUnsuspended)
	testenv.Eventually(t, 10*time.Second, func() error {
		if !dispatched(t, db, e.ID) {
			return errors.New("not marked dispatched")
		}
		return nil
	})
}

// With no notification, the one-second poll still delivers.
func TestDeliversWithoutNotify(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	a := events.NewInbox(p, "A")
	bus := newBus(t, db, p, a)
	bus.Subscribe(events.AccountUpdated, a.ServiceName(), "Handle")
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO events (id, kind, subject_id, payload, occurred_at) VALUES (@id, 'account.updated', @id, '{}', now())`,
		pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	run(t, bus)
	testenv.Eventually(t, 10*time.Second, func() error {
		if n := a.Count(id); n != 1 {
			return fmt.Errorf("delivered %d times, want 1", n)
		}
		return nil
	})
}

// While Restate cannot be reached, events stay pending; a bus that can
// reach it delivers them.
func TestDeliversAfterRestateReturns(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	a := events.NewInbox(p, "A")
	down := events.NewBus(db, ingress.NewClient("http://127.0.0.1:1"), p, time.Now, discard)
	down.Subscribe(events.AccountUpdated, a.ServiceName(), "Handle")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = down.Run(ctx); close(done) }()
	e := publish(t, db, events.AccountUpdated)
	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-done
	if dispatched(t, db, e.ID) {
		t.Fatal("marked while Restate was down")
	}
	up := newBus(t, db, p, a)
	up.Subscribe(events.AccountUpdated, a.ServiceName(), "Handle")
	run(t, up)
	testenv.Eventually(t, 10*time.Second, func() error {
		if n, d := a.Count(e.ID), dispatched(t, db, e.ID); n != 1 || !d {
			return fmt.Errorf("delivered %d times, dispatched %t", n, d)
		}
		return nil
	})
}

func TestOldDispatchedEventsAreDeleted(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	now := time.Now()
	old, recent := uuid.NewV7(), uuid.NewV7()
	for id, at := range map[uuid.UUID]time.Time{old: now.Add(-8 * 24 * time.Hour), recent: now.Add(-6 * 24 * time.Hour)} {
		if _, err := db.Exec(context.Background(),
			`INSERT INTO events (id, kind, subject_id, payload, occurred_at, dispatched_at) VALUES (@id, 'account.updated', @id, '{}', @at, @at)`,
			pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
			t.Fatal(err)
		}
	}
	run(t, events.NewBus(db, restatetest.Ingress(t), restatetest.Prefix(), func() time.Time { return now }, discard))
	count := func(id uuid.UUID) int {
		var n int
		_ = db.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&n)
		return n
	}
	testenv.Eventually(t, 10*time.Second, func() error {
		if count(old) != 0 {
			return errors.New("the old event is still there")
		}
		return nil
	})
	if count(recent) != 1 {
		t.Fatal("a recent event was deleted")
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	t.Parallel()
	bus := events.NewBus(pgtest.New(t), restatetest.Ingress(t), restatetest.Prefix(), time.Now, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestSubscribeAfterRunPanics(t *testing.T) {
	t.Parallel()
	bus := events.NewBus(pgtest.New(t), restatetest.Ingress(t), restatetest.Prefix(), time.Now, discard)
	run(t, bus)
	time.Sleep(100 * time.Millisecond)
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	bus.Subscribe(events.AccountUpdated, "late", "Handle")
}

// Run listens on a connection of its own, so closing the pool while Run
// waits for notifications does not hang.
func TestPoolClosesWhileRunning(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	run(t, events.NewBus(db, restatetest.Ingress(t), restatetest.Prefix(), time.Now, discard))
	time.Sleep(200 * time.Millisecond) // listening by now
	closed := make(chan struct{})
	go func() { db.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("pool.Close hangs while the bus listens")
	}
}

// A subscriber Restate does not know delays only the events sent to it:
// Restate retries that Deliver, and every other event is delivered.
func TestUnknownSubscriberDelaysOnlyItsEvents(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	a := events.NewInbox(p, "A")
	bus := newBus(t, db, p, a)
	bus.Subscribe(events.AccountUpdated, p+"NoSuchService", "Handle")
	bus.Subscribe(events.AccountSuspended, a.ServiceName(), "Handle")
	publish(t, db, events.AccountUpdated)
	later := publish(t, db, events.AccountSuspended)
	run(t, bus)
	testenv.Eventually(t, 10*time.Second, func() error {
		if n := a.Count(later.ID); n != 1 {
			return fmt.Errorf("delivered %d times, want 1", n)
		}
		return nil
	})
}

func TestCheckRefusesUnknownSubscriptions(t *testing.T) {
	t.Parallel()
	p := restatetest.Prefix()
	a := events.NewInbox(p, "A")
	for name, sub := range map[string][2]string{
		"unknown service": {p + "NoSuchService", "Handle"},
		"unknown handler": {a.ServiceName(), "NoSuchHandler"},
	} {
		bus := events.NewBus(nil, nil, p, time.Now, discard)
		bus.Subscribe(events.AccountUpdated, sub[0], sub[1])
		if err := bus.Check([]restate.ServiceDefinition{bus.Definition(), restate.Reflect(a)}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	ok := events.NewBus(nil, nil, p, time.Now, discard)
	ok.Subscribe(events.AccountUpdated, a.ServiceName(), "Handle")
	if err := ok.Check([]restate.ServiceDefinition{restate.Reflect(a)}); err == nil {
		t.Error("no error without the bus's own Events service")
	}
	if err := ok.Check([]restate.ServiceDefinition{ok.Definition(), restate.Reflect(a)}); err != nil {
		t.Fatal(err)
	}
}

// When Run returns, its listen connection is closed, not left open.
func TestRunClosesItsListenConnection(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	bus := events.NewBus(db, restatetest.Ingress(t), restatetest.Prefix(), time.Now, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = bus.Run(ctx); close(done) }()
	listening := func() int {
		var n int
		if err := db.QueryRow(context.Background(), `
SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND query = 'LISTEN events'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	testenv.Eventually(t, 10*time.Second, func() error {
		if n := listening(); n != 1 {
			return fmt.Errorf("%d listen connections, want 1", n)
		}
		return nil
	})
	cancel()
	<-done
	// Postgres drops the backend a moment after the client closes it.
	testenv.Eventually(t, 10*time.Second, func() error {
		if n := listening(); n != 0 {
			return fmt.Errorf("%d listen connections, want 0", n)
		}
		return nil
	})
}

// Of two subscribers of a kind, the first gets the event once while the
// second is not registered yet, and still once after the second is: the
// retried Deliver does not send to the first again.
func TestLateSubscriberDoesNotRepeatTheFirst(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	p := restatetest.Prefix()
	first, late := events.NewInbox(p, "First"), events.NewInbox(p, "Late")
	bus := newBus(t, db, p, first)
	bus.Subscribe(events.AccountUpdated, first.ServiceName(), "Handle")
	bus.Subscribe(events.AccountUpdated, late.ServiceName(), "Handle")
	run(t, bus)
	e := publish(t, db, events.AccountUpdated)
	testenv.Eventually(t, 10*time.Second, func() error {
		if n := first.Count(e.ID); n != 1 {
			return fmt.Errorf("first got it %d times, want 1", n)
		}
		return nil
	})
	time.Sleep(5 * time.Second) // Restate retries Deliver meanwhile
	restatetest.Start(t, restate.Reflect(late))
	// Restate's backoff grows past a minute.
	testenv.Eventually(t, 90*time.Second, func() error {
		if late.Count(e.ID) == 0 {
			return errors.New("late got nothing")
		}
		return nil
	})
	if late.Count(e.ID) != 1 || first.Count(e.ID) != 1 {
		t.Fatalf("first %d, late %d", first.Count(e.ID), late.Count(e.ID))
	}
}

package events_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

var discard = slog.New(slog.DiscardHandler)

// inbox is a subscriber that counts the events it handled.
type inbox struct {
	prefix string
	mu     *sync.Mutex
	got    map[uuid.UUID]int
}

func newInbox() inbox {
	return inbox{prefix: restatetest.Prefix(), mu: &sync.Mutex{}, got: map[uuid.UUID]int{}}
}

func (i inbox) ServiceName() string { return i.prefix + "Inbox" }

func (i inbox) Handle(ctx restate.Context, e events.Event) error {
	return restate.RunVoid(ctx, func(restate.RunContext) error {
		i.mu.Lock()
		defer i.mu.Unlock()
		i.got[e.ID]++
		return nil
	})
}

func (i inbox) count(id uuid.UUID) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.got[id]
}

// eventually fails t unless ok turns true within ten seconds.
func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("not within 10s")
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
	e := events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: uuid.NewV7(), At: time.Now()}
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
	a, b := newInbox(), newInbox()
	in := restatetest.Start(t, restate.Reflect(a), restate.Reflect(b))
	bus := events.NewBus(db, in, time.Now, discard)
	bus.Subscribe(events.AccountSuspended, a.ServiceName(), "Handle")
	bus.Subscribe(events.AccountSuspended, b.ServiceName(), "Handle")
	run(t, bus)
	e := publish(t, db, events.AccountSuspended)
	eventually(t, func() bool { return a.count(e.ID) == 1 && b.count(e.ID) == 1 && dispatched(t, db, e.ID) })
	time.Sleep(1500 * time.Millisecond) // one more poll round
	if a.count(e.ID) != 1 || b.count(e.ID) != 1 {
		t.Fatalf("delivered %d and %d times", a.count(e.ID), b.count(e.ID))
	}
}

func TestTwoBusesDeliverEachEventOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := newInbox()
	in := restatetest.Start(t, restate.Reflect(a))
	for range 2 {
		bus := events.NewBus(db, in, time.Now, discard)
		bus.Subscribe(events.AccountRenamed, a.ServiceName(), "Handle")
		run(t, bus)
	}
	sent := make([]events.Event, 0, 50)
	for range 50 {
		sent = append(sent, publish(t, db, events.AccountRenamed))
	}
	eventually(t, func() bool {
		for _, e := range sent {
			if a.count(e.ID) == 0 {
				return false
			}
		}
		return true
	})
	for _, e := range sent {
		if n := a.count(e.ID); n != 1 {
			t.Fatalf("%s delivered %d times", e.ID, n)
		}
	}
}

func TestEventWithoutSubscriberIsMarked(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	bus := events.NewBus(db, restatetest.Start(t), time.Now, discard)
	run(t, bus)
	e := publish(t, db, events.AccountUnsuspended)
	eventually(t, func() bool { return dispatched(t, db, e.ID) })
}

// With no notification, the one-second poll still delivers.
func TestDeliversWithoutNotify(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := newInbox()
	in := restatetest.Start(t, restate.Reflect(a))
	bus := events.NewBus(db, in, time.Now, discard)
	bus.Subscribe(events.AccountRenamed, a.ServiceName(), "Handle")
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO events (id, kind, subject_id, data, at) VALUES (@id, 'account.renamed', @id, '{}', now())`,
		pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	run(t, bus)
	eventually(t, func() bool { return a.count(id) == 1 })
}

// While Restate cannot be reached, events stay pending; a bus that can
// reach it delivers them.
func TestDeliversAfterRestateReturns(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := newInbox()
	in := restatetest.Start(t, restate.Reflect(a))
	down := events.NewBus(db, ingress.NewClient("http://127.0.0.1:1"), time.Now, discard)
	down.Subscribe(events.AccountRenamed, a.ServiceName(), "Handle")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = down.Run(ctx); close(done) }()
	e := publish(t, db, events.AccountRenamed)
	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-done
	if dispatched(t, db, e.ID) {
		t.Fatal("marked while Restate was down")
	}
	up := events.NewBus(db, in, time.Now, discard)
	up.Subscribe(events.AccountRenamed, a.ServiceName(), "Handle")
	run(t, up)
	eventually(t, func() bool { return a.count(e.ID) == 1 && dispatched(t, db, e.ID) })
}

func TestOldDispatchedEventsAreDeleted(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	now := time.Now()
	old, recent := uuid.NewV7(), uuid.NewV7()
	for id, at := range map[uuid.UUID]time.Time{old: now.Add(-8 * 24 * time.Hour), recent: now.Add(-6 * 24 * time.Hour)} {
		if _, err := db.Exec(context.Background(),
			`INSERT INTO events (id, kind, subject_id, data, at, dispatched_at) VALUES (@id, 'account.renamed', @id, '{}', @at, @at)`,
			pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
			t.Fatal(err)
		}
	}
	run(t, events.NewBus(db, restatetest.Start(t), func() time.Time { return now }, discard))
	count := func(id uuid.UUID) int {
		var n int
		_ = db.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&n)
		return n
	}
	eventually(t, func() bool { return count(old) == 0 })
	if count(recent) != 1 {
		t.Fatal("a recent event was deleted")
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	t.Parallel()
	bus := events.NewBus(pgtest.New(t), restatetest.Start(t), time.Now, discard)
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
	bus := events.NewBus(pgtest.New(t), restatetest.Start(t), time.Now, discard)
	run(t, bus)
	time.Sleep(100 * time.Millisecond)
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	bus.Subscribe(events.AccountRenamed, "late", "Handle")
}

// Run listens on a connection of its own, so closing the pool while Run
// waits for notifications does not hang.
func TestPoolClosesWhileRunning(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	run(t, events.NewBus(db, restatetest.Start(t), time.Now, discard))
	time.Sleep(200 * time.Millisecond) // listening by now
	closed := make(chan struct{})
	go func() { db.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("pool.Close hangs while the bus listens")
	}
}

// A subscription Restate refuses (a misspelled service) leaves its event
// pending and delivers every other event.
func TestUndeliverableEventBlocksNoOther(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := newInbox()
	in := restatetest.Start(t, restate.Reflect(a))
	bus := events.NewBus(db, in, time.Now, discard)
	bus.Subscribe(events.AccountRenamed, a.prefix+"NoSuchService", "Handle")
	bus.Subscribe(events.AccountSuspended, a.ServiceName(), "Handle")
	stuck := publish(t, db, events.AccountRenamed)
	later := publish(t, db, events.AccountSuspended)
	run(t, bus)
	eventually(t, func() bool { return a.count(later.ID) == 1 })
	if dispatched(t, db, stuck.ID) {
		t.Fatal("an undelivered event was marked")
	}
}

func TestCheckRefusesUnknownSubscriptions(t *testing.T) {
	t.Parallel()
	a := newInbox()
	defs := []restate.ServiceDefinition{restate.Reflect(a)}
	for name, sub := range map[string][2]string{
		"unknown service": {a.prefix + "NoSuchService", "Handle"},
		"unknown handler": {a.ServiceName(), "NoSuchHandler"},
	} {
		bus := events.NewBus(nil, nil, time.Now, discard)
		bus.Subscribe(events.AccountRenamed, sub[0], sub[1])
		if err := bus.Check(defs); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	ok := events.NewBus(nil, nil, time.Now, discard)
	ok.Subscribe(events.AccountRenamed, a.ServiceName(), "Handle")
	if err := ok.Check(defs); err != nil {
		t.Fatal(err)
	}
}

// When Run returns, its listen connection is closed, not left open.
func TestRunClosesItsListenConnection(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	bus := events.NewBus(db, restatetest.Start(t), time.Now, discard)
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
	eventually(t, func() bool { return listening() == 1 })
	cancel()
	<-done
	// Postgres drops the backend a moment after the client closes it.
	eventually(t, func() bool { return listening() == 0 })
}

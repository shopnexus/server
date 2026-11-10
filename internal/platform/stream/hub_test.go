package stream_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/stream"
)

// fakeBell hands the test the hub's deliver and resync, and records typing
// rings.
type fakeBell struct {
	mu        sync.Mutex
	deliver   func([]stream.Target, stream.Hint)
	resync    func()
	listening chan struct{}
	rung      []stream.Target
}

func (b *fakeBell) Ring(_ context.Context, _ pgx.Tx, targets []stream.Target, _ stream.Hint) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rung = append(b.rung, targets...)
	return nil
}

func (b *fakeBell) Listen(ctx context.Context, deliver func([]stream.Target, stream.Hint), resync func()) {
	b.mu.Lock()
	b.deliver, b.resync = deliver, resync
	b.mu.Unlock()
	close(b.listening)
	<-ctx.Done()
}

type fakeLog struct{ pruned atomic.Int32 }

func (*fakeLog) Current(context.Context, uuid.UUID) (int64, error) { return 0, nil }

func (*fakeLog) Since(context.Context, uuid.UUID, int64, int) (stream.Page, error) {
	return stream.Page{Updates: []stream.Update{}}, nil
}

func (l *fakeLog) Prune(context.Context, time.Time) (int64, error) {
	l.pruned.Add(1)
	return 0, nil
}

func runningHub(t *testing.T) (*stream.Hub, *fakeBell, *fakeLog) {
	t.Helper()
	bell, log := &fakeBell{listening: make(chan struct{})}, &fakeLog{}
	h := stream.NewHub(bell, log, time.Now, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = h.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	<-bell.listening
	return h, bell, log
}

func gets(t *testing.T, s *stream.Sub, want stream.Event) {
	t.Helper()
	select {
	case got := <-s.Events():
		if got.Seq != want.Seq || got.Hint.Kind != want.Hint.Kind {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no event within 1s")
	}
}

func noEvent(t *testing.T, s *stream.Sub) {
	t.Helper()
	select {
	case got := <-s.Events():
		t.Fatalf("an unexpected event: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func behind(t *testing.T, s *stream.Sub, want bool) {
	t.Helper()
	select {
	case <-s.Behind():
		if !want {
			t.Fatal("behind, want not")
		}
	case <-time.After(200 * time.Millisecond):
		if want {
			t.Fatal("not behind, want behind")
		}
	}
}

func TestEventsReachEveryConnectionOfTheAccount(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	a, b := uuid.NewV7(), uuid.NewV7()
	phone, stopPhone := h.Subscribe(a)
	defer stopPhone()
	tablet, stopTablet := h.Subscribe(a)
	defer stopTablet()
	other, stopOther := h.Subscribe(b)
	defer stopOther()
	hint := dealHint()
	bell.deliver([]stream.Target{{Account: a, Seq: 1}}, hint)
	gets(t, phone, stream.Event{Seq: 1, Hint: hint})
	gets(t, tablet, stream.Event{Seq: 1, Hint: hint})
	noEvent(t, other)
}

func TestStoppedSubscriptionGetsNothing(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	a := uuid.NewV7()
	s, stop := h.Subscribe(a)
	stop()
	bell.deliver([]stream.Target{{Account: a, Seq: 1}}, dealHint())
	noEvent(t, s)
}

func TestFullQueueFallsBehind(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	a := uuid.NewV7()
	s, stop := h.Subscribe(a)
	defer stop()
	for seq := range int64(16) {
		bell.deliver([]stream.Target{{Account: a, Seq: seq + 1}}, dealHint())
	}
	behind(t, s, false)
	bell.deliver([]stream.Target{{Account: a, Seq: 17}}, dealHint())
	behind(t, s, true)
}

func TestTypingOverAFullQueueIsDropped(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	a := uuid.NewV7()
	s, stop := h.Subscribe(a)
	defer stop()
	conv := uuid.NewV7()
	typing := stream.Hint{Kind: stream.KindTyping, ConversationID: &conv}
	for range 17 {
		bell.deliver([]stream.Target{{Account: a}}, typing)
	}
	behind(t, s, false)
	if n := len(s.Events()); n != 16 {
		t.Fatalf("%d queued", n)
	}
}

func TestResyncMarksEveryConnectionBehind(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	sa, stopA := h.Subscribe(uuid.NewV7())
	defer stopA()
	sb, stopB := h.Subscribe(uuid.NewV7())
	defer stopB()
	bell.resync()
	behind(t, sa, true)
	behind(t, sb, true)
}

func TestTypingRingsTheOtherMember(t *testing.T) {
	t.Parallel()
	h, bell, _ := runningHub(t)
	to := uuid.NewV7()
	if err := h.Typing(context.Background(), to, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	bell.mu.Lock()
	defer bell.mu.Unlock()
	if len(bell.rung) != 1 || bell.rung[0].Account != to || bell.rung[0].Seq != 0 {
		t.Fatalf("%+v", bell.rung)
	}
}

func TestCloseClosesDone(t *testing.T) {
	t.Parallel()
	h, _, _ := runningHub(t)
	h.Close()
	h.Close()
	select {
	case <-h.Done():
	default:
		t.Fatal("Done open after Close")
	}
}

func TestRunPrunesAtStart(t *testing.T) {
	t.Parallel()
	_, _, log := runningHub(t)
	deadline := time.Now().Add(time.Second)
	for log.pruned.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no prune within 1s of start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

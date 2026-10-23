package restatex_test

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/restatetest"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// attempt is what one run of the handler saw before its failing step.
type attempt struct {
	at time.Time
	id uuid.UUID
}

// replayed fails its step on the first run, so Restate runs the handler
// again from the journal.
type replayed struct {
	prefix   string
	ticks    *atomic.Int64
	failed   *atomic.Bool
	mu       *sync.Mutex
	attempts *[]attempt
}

func (s replayed) ServiceName() string { return s.prefix + "Replayed" }

func (s replayed) Run(ctx restate.Context, _ string) (string, error) {
	// Each call of now returns a later time, so a second reading would differ.
	now := func() time.Time {
		return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC).Add(time.Duration(s.ticks.Add(1)) * time.Second)
	}
	at, err := restatex.Now(ctx, now)
	if err != nil {
		return "", err
	}
	id := restatex.NewID(ctx, at)
	s.mu.Lock()
	*s.attempts = append(*s.attempts, attempt{at: at, id: id})
	s.mu.Unlock()
	if err := restate.RunVoid(ctx, func(restate.RunContext) error {
		if !s.failed.Swap(true) {
			return errors.New("fail once")
		}
		return nil
	}); err != nil {
		return "", err
	}
	return id.String(), nil
}

func TestNowAndNewIDSurviveAReplay(t *testing.T) {
	t.Parallel()
	var attempts []attempt
	svc := replayed{
		prefix: restatetest.Prefix(), ticks: &atomic.Int64{}, failed: &atomic.Bool{},
		mu: &sync.Mutex{}, attempts: &attempts,
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	got, err := ingress.Service[string, string](client, svc.ServiceName(), "Run").
		Request(t.Context(), "", restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(attempts) < 2 {
		t.Fatalf("the handler ran %d times; the failing step should have made Restate run it again", len(attempts))
	}
	first := attempts[0]
	for i, a := range attempts[1:] {
		if a != first {
			t.Fatalf("attempt %d saw %v %v, the first saw %v %v", i+2, a.at, a.id, first.at, first.id)
		}
	}
	if got != first.id.String() {
		t.Fatalf("returned %s, minted %s", got, first.id)
	}
	if v := first.id[6] >> 4; v != 7 {
		t.Fatalf("version %d, want 7", v)
	}
	if first.id[8]>>6 != 0b10 {
		t.Fatalf("variant bits %02b, want 10", first.id[8]>>6)
	}
	var ms [8]byte
	copy(ms[2:], first.id[0:6])
	if int64(binary.BigEndian.Uint64(ms[:])) != first.at.UnixMilli() {
		t.Fatal("the first 48 bits are not the journaled time in milliseconds")
	}
}

func TestOutcome(t *testing.T) {
	t.Parallel()
	target := errors.New("lost")
	other := errors.New("broken")
	cases := []struct {
		name    string
		err     error
		want    bool
		wantErr error
	}{
		{"nil", nil, false, nil},
		{"target", target, true, nil},
		{"wrapped target", errors.Join(errors.New("save"), target), true, nil},
		{"other", other, false, other},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := restatex.Outcome(c.err, target)
			if got != c.want || !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestIgnore(t *testing.T) {
	t.Parallel()
	target := errors.New("gone")
	other := errors.New("broken")
	if err := restatex.Ignore(target, target); err != nil {
		t.Fatalf("target: %v", err)
	}
	if err := restatex.Ignore(other, target); !errors.Is(err, other) {
		t.Fatalf("other: %v", err)
	}
	if err := restatex.Ignore(nil, target); err != nil {
		t.Fatalf("nil: %v", err)
	}
}

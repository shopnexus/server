package bifrost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// The metadata the server's reconciler writes on its virtual keys. A key
// with owner=server is the server's; the rest say what its caller calls.
const (
	MetaOwner     = "owner"
	OwnerServer   = "server"
	MetaAgent     = "agent"
	MetaModel     = "model"
	MetaFallbacks = "fallbacks"
	MetaExtraBody = "extra_body"
)

// ErrNoKey is a caller Bifrost has no server key for.
var ErrNoKey = errors.New("bifrost: no virtual key for the caller")

// Route is what a caller sends: its key, its agent's model and fallbacks
// as provider/model, and the fields its requests carry beside them.
type Route struct {
	Key       string
	Model     string
	Fallbacks []string
	ExtraBody map[string]any
}

// RouteOf reads a server key's route from its metadata; false for a key
// that is not the server's.
func RouteOf(k VirtualKey) (Route, bool) {
	if k.Metadata[MetaOwner] != OwnerServer {
		return Route{}, false
	}
	r := Route{Key: k.Value, Model: k.Metadata[MetaModel], Fallbacks: SplitList(k.Metadata[MetaFallbacks])}
	if raw := k.Metadata[MetaExtraBody]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &r.ExtraBody)
	}
	return r, true
}

// JoinList and SplitList keep a list in one metadata value.
func JoinList(list []string) string { return strings.Join(list, ",") }

func SplitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// How the ring reads Bifrost: a route older than ringTTL is served while
// a read runs behind it; a caller the ring lacks waits for a read, at most
// one per missWait; a failed read is not tried again for failBackoff; a
// forced read (a key Bifrost refused) runs at most once per reloadEvery.
const (
	ringTTL     = 5 * time.Minute
	missWait    = 10 * time.Second
	failBackoff = 5 * time.Second
	reloadEvery = 10 * time.Second
	readTimeout = 30 * time.Second
)

// Ring keeps the server's virtual keys in memory, by caller (the key's
// name), read from Bifrost. No key is stored anywhere else.
type Ring struct {
	admin Admin
	now   func() time.Time

	mu       sync.Mutex
	routes   map[string]Route
	at       time.Time     // the last good read
	failedAt time.Time     // the last failed read
	failErr  error         // its error
	forcedAt time.Time     // the last Reload that read
	reading  chan struct{} // closed when the read in flight ends
	onMiss   func(caller string)
}

func NewRing(a Admin, now func() time.Time) *Ring { return &Ring{admin: a, now: now} }

// OnMiss sets what runs when a read finds no key for a caller, such as a
// reconcile that makes it. It runs in its own goroutine.
func (r *Ring) OnMiss(f func(caller string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onMiss = f
}

// Route is caller's route. A route the ring has comes back at once, and a
// stale one starts a read behind it; a caller the ring lacks waits for a
// read, or for ctx.
func (r *Ring) Route(ctx context.Context, caller string) (Route, error) {
	r.mu.Lock()
	route, ok := r.routes[caller]
	now := r.now()
	loaded := r.routes != nil
	stale := now.Sub(r.at) >= ringTTL
	recent := loaded && now.Sub(r.at) < missWait
	r.mu.Unlock()
	if ok {
		if stale {
			r.start()
		}
		return route, nil
	}
	if !recent {
		if err := r.wait(ctx, r.start()); err != nil {
			return Route{}, err
		}
	}
	r.mu.Lock()
	route, ok = r.routes[caller]
	miss := r.onMiss
	r.mu.Unlock()
	if !ok {
		if miss != nil {
			go miss(caller)
		}
		return Route{}, fmt.Errorf("%w: %s", ErrNoKey, caller)
	}
	return route, nil
}

// Reload reads Bifrost again after it refused a key, at most once per
// reloadEvery; it waits for the read, or for ctx.
func (r *Ring) Reload(ctx context.Context) error {
	r.mu.Lock()
	now := r.now()
	recent := !r.forcedAt.IsZero() && now.Sub(r.forcedAt) < reloadEvery
	if !recent {
		r.forcedAt = now
	}
	r.mu.Unlock()
	if recent {
		return nil
	}
	return r.wait(ctx, r.start())
}

// start begins a read unless one is in flight or the last one failed
// within failBackoff, and returns what ends when it is done.
func (r *Ring) start() chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reading != nil {
		return r.reading
	}
	done := make(chan struct{})
	if r.failErr != nil && r.now().Sub(r.failedAt) < failBackoff {
		close(done)
		return done
	}
	r.reading = done
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		keys, err := r.admin.VirtualKeys(ctx)
		if err == nil {
			r.Set(keys)
		}
		r.mu.Lock()
		if err != nil {
			r.failedAt, r.failErr = r.now(), err
		}
		r.reading = nil
		r.mu.Unlock()
		close(done)
	}()
	return done
}

// wait waits for done or ctx, then gives the last read's error when the
// ring has nothing.
func (r *Ring) wait(ctx context.Context, done chan struct{}) error {
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("read the virtual keys: %w", ctx.Err())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failErr != nil && r.failedAt.After(r.at) {
		return fmt.Errorf("read the virtual keys: %w", r.failErr)
	}
	return nil
}

// Set replaces the ring's routes with those of keys, a listing just read.
func (r *Ring) Set(keys []VirtualKey) {
	routes := map[string]Route{}
	for _, k := range keys {
		if route, ok := RouteOf(k); ok && k.Active() {
			routes[k.Name] = route
		}
	}
	r.mu.Lock()
	r.routes, r.at, r.failErr = routes, r.now(), nil
	r.mu.Unlock()
}

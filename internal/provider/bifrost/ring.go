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

// ringTTL is how long the ring trusts what it read before reading again,
// and missWait how long a caller it lacks waits before the next read.
const (
	ringTTL  = 5 * time.Minute
	missWait = 10 * time.Second
)

// Ring keeps the server's virtual keys in memory, by caller (the key's
// name), read from Bifrost. No key is stored anywhere else.
type Ring struct {
	admin Admin
	now   func() time.Time
	// loading lets one read of Bifrost run at a time.
	loading sync.Mutex
	mu      sync.Mutex
	routes  map[string]Route
	at      time.Time
	// reads counts the listings read, so a reader that waited knows one
	// finished while it waited.
	reads int
}

func NewRing(a Admin, now func() time.Time) *Ring { return &Ring{admin: a, now: now} }

// Route is caller's route, read again when the ring is older than its TTL;
// a failed read serves the last one it had.
func (r *Ring) Route(ctx context.Context, caller string) (Route, error) {
	r.mu.Lock()
	route, ok := r.routes[caller]
	age := r.now().Sub(r.at)
	loaded := r.routes != nil
	r.mu.Unlock()
	if ok && age < ringTTL {
		return route, nil
	}
	if !ok && loaded && age < missWait {
		return Route{}, fmt.Errorf("%w: %s", ErrNoKey, caller)
	}
	if err := r.Reload(ctx); err != nil {
		if ok {
			return route, nil
		}
		return Route{}, err
	}
	r.mu.Lock()
	route, ok = r.routes[caller]
	r.mu.Unlock()
	if !ok {
		return Route{}, fmt.Errorf("%w: %s", ErrNoKey, caller)
	}
	return route, nil
}

// Reload reads the server's keys from Bifrost. Readers that wait for one
// in flight take its result.
func (r *Ring) Reload(ctx context.Context) error {
	r.mu.Lock()
	asked := r.reads
	r.mu.Unlock()
	r.loading.Lock()
	defer r.loading.Unlock()
	r.mu.Lock()
	done := r.reads != asked
	r.mu.Unlock()
	if done {
		return nil
	}
	keys, err := r.admin.VirtualKeys(ctx)
	if err != nil {
		return err
	}
	r.Set(keys)
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
	r.routes, r.at = routes, r.now()
	r.reads++
	r.mu.Unlock()
}

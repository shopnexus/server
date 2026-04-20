package redislocker

import (
	"context"
	"log/slog"
	"shopnexus-server/internal/infras/locker"
	"sort"
	"time"

	"github.com/redis/rueidis"
)

var _ locker.Client = (*RedisLocker)(nil)

const (
	defaultTTL      = 30 * time.Second
	waitTimeoutSec  = 0.5 // BLPOP fallback timeout — safety net if a signal is missed.
	signalListTTLMs = 500 // PEXPIRE on the signal list to prevent leaked elements.
)

// RedisLocker implements a distributed RWMutex using Redis.
//
// Lock (exclusive/write): SET NX on "lock:{key}" — only one writer at a time,
// blocks while any readers hold "rlock:{key}" (counter > 0).
//
// RLock (shared/read): INCR on "rlock:{key}" — multiple readers allowed,
// blocks while a writer holds "lock:{key}".
//
// Waiting is done via BLPOP on "wait:{key}" (server-side blocking, FIFO).
// Holders signal the next waiter on release via RPUSH + PEXPIRE. This yields
// bounded, queue-order latency instead of bimodal polling latency.
//
// Both auto-renew their TTL every ttl/2 to prevent expiry during long operations.
type RedisLocker struct {
	rdb rueidis.Client
	cfg locker.Config
}

func NewRedisLocker(rdb rueidis.Client, cfg locker.Config) *RedisLocker {
	if cfg.TTL <= 0 {
		cfg.TTL = defaultTTL
	}
	return &RedisLocker{rdb: rdb, cfg: cfg}
}

// Lock acquires one or more exclusive (write) locks. Keys are sorted internally
// and acquired in deterministic order to prevent deadlocks. Returns a single
// unlock func that releases all acquired locks in reverse order.
func (l *RedisLocker) Lock(ctx context.Context, keys ...string) func() {
	if len(keys) == 0 {
		return func() {}
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)

	unlocks := make([]func(), 0, len(sorted))
	for _, k := range sorted {
		unlocks = append(unlocks, l.lockOne(ctx, k))
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
}

// lockOne acquires a single exclusive lock. Waits for all readers to release,
// then acquires the write lock. Returns unlock func.
func (l *RedisLocker) lockOne(ctx context.Context, key string) func() {
	lockKey := "lock:" + key
	rlockKey := "rlock:" + key
	waitKey := "wait:" + key

	// Step 1: acquire exclusive lock (SET NX); BLPOP while held.
	for {
		resp := l.rdb.Do(ctx, l.rdb.B().Set().Key(lockKey).Value("1").Nx().Ex(l.cfg.TTL).Build())
		if resp.Error() == nil {
			break
		}
		if ctx.Err() != nil {
			slog.Warn("lock: context cancelled", slog.String("key", key))
			return func() {}
		}
		_ = l.rdb.Do(ctx, l.rdb.B().Blpop().Key(waitKey).Timeout(waitTimeoutSec).Build())
	}

	// Step 2: wait for all readers to finish (rlock counter = 0).
	for {
		count, err := l.rdb.Do(ctx, l.rdb.B().Get().Key(rlockKey).Build()).AsInt64()
		if err != nil || count <= 0 {
			break
		}
		if ctx.Err() != nil {
			bg := context.Background()
			l.rdb.Do(bg, l.rdb.B().Del().Key(lockKey).Build())
			l.signalWaiters(bg, waitKey)
			return func() {}
		}
		_ = l.rdb.Do(ctx, l.rdb.B().Blpop().Key(waitKey).Timeout(waitTimeoutSec).Build())
	}

	// Auto-renew TTL.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(l.cfg.TTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.rdb.Do(ctx, l.rdb.B().Expire().Key(lockKey).Seconds(int64(l.cfg.TTL.Seconds())).Build())
			}
		}
	}()

	return func() {
		close(done)
		bg := context.Background()
		l.rdb.Do(bg, l.rdb.B().Del().Key(lockKey).Build())
		l.signalWaiters(bg, waitKey)
	}
}

// RLock acquires one or more shared (read) locks. Keys are sorted internally.
// Returns a single unlock func that releases all acquired locks in reverse order.
func (l *RedisLocker) RLock(ctx context.Context, keys ...string) func() {
	if len(keys) == 0 {
		return func() {}
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)

	unlocks := make([]func(), 0, len(sorted))
	for _, k := range sorted {
		unlocks = append(unlocks, l.rlockOne(ctx, k))
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
}

// rlockOne acquires a single shared (read) lock. Multiple readers can hold simultaneously.
// Blocks while a writer holds the exclusive lock. Returns unlock func.
func (l *RedisLocker) rlockOne(ctx context.Context, key string) func() {
	lockKey := "lock:" + key
	rlockKey := "rlock:" + key
	waitKey := "wait:" + key

	// Wait for no active writer.
	for {
		exists, _ := l.rdb.Do(ctx, l.rdb.B().Exists().Key(lockKey).Build()).AsBool()
		if !exists {
			break
		}
		if ctx.Err() != nil {
			slog.Warn("rlock: context cancelled", slog.String("key", key))
			return func() {}
		}
		_ = l.rdb.Do(ctx, l.rdb.B().Blpop().Key(waitKey).Timeout(waitTimeoutSec).Build())
	}

	// Increment reader counter with TTL.
	l.rdb.Do(ctx, l.rdb.B().Incr().Key(rlockKey).Build())
	l.rdb.Do(ctx, l.rdb.B().Expire().Key(rlockKey).Seconds(int64(l.cfg.TTL.Seconds())).Build())

	// Cascade: wake the next waiter. If it's another reader, they'll also acquire
	// and cascade — yielding parallel reader acquisition after a writer releases.
	l.signalWaiters(ctx, waitKey)

	// Auto-renew reader TTL.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(l.cfg.TTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.rdb.Do(ctx, l.rdb.B().Expire().Key(rlockKey).Seconds(int64(l.cfg.TTL.Seconds())).Build())
			}
		}
	}()

	return func() {
		close(done)
		bg := context.Background()
		count, _ := l.rdb.Do(bg, l.rdb.B().Decr().Key(rlockKey).Build()).AsInt64()
		if count <= 0 {
			l.signalWaiters(bg, waitKey)
		}
	}
}

// signalWaiters wakes one BLPOP waiter. The PEXPIRE keeps the list from
// accumulating stale signals if no one picks them up within ~500ms.
func (l *RedisLocker) signalWaiters(ctx context.Context, waitKey string) {
	l.rdb.Do(ctx, l.rdb.B().Rpush().Key(waitKey).Element("1").Build())
	l.rdb.Do(ctx, l.rdb.B().Pexpire().Key(waitKey).Milliseconds(signalListTTLMs).Build())
}

package locker

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/redis/rueidis"
)

var _ Client = (*RedisLocker)(nil)

// RedisLocker implements distributed RWMutex using Redis.
//
// Lock (exclusive/write): SET NX on "lock:{key}" — only one writer at a time,
// blocks while any readers hold "rlock:{key}" (counter > 0).
//
// RLock (shared/read): INCR on "rlock:{key}" — multiple readers allowed,
// blocks while a writer holds "lock:{key}".
//
// Both auto-renew their TTL every ttl/2 to prevent expiry during long operations.
type RedisLocker struct {
	rdb rueidis.Client
	cfg Config
}

func NewRedisLocker(rdb rueidis.Client, cfg Config) *RedisLocker {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Second
	}
	return &RedisLocker{rdb: rdb, cfg: cfg}
}

// Lock acquires one or more exclusive (write) locks. Keys are sorted internally
// and acquired in deterministic order to prevent deadlocks. Returns a single
// unlock func that releases all acquired locks in reverse order.
func (l *RedisLocker) Lock(ctx context.Context, keys ...string) (unlock func()) {
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
func (l *RedisLocker) lockOne(ctx context.Context, key string) (unlock func()) {
	lockKey := "lock:" + key
	rlockKey := "rlock:" + key

	// Step 1: Acquire exclusive lock (SET NX)
	for {
		resp := l.rdb.Do(ctx, l.rdb.B().Set().Key(lockKey).Value("1").Nx().Ex(l.cfg.TTL).Build())
		if resp.Error() == nil {
			break
		}
		if ctx.Err() != nil {
			slog.Warn("lock: context cancelled", slog.String("key", key))
			return func() {}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Step 2: Wait for all readers to finish (rlock counter = 0)
	for {
		count, err := l.rdb.Do(ctx, l.rdb.B().Get().Key(rlockKey).Build()).AsInt64()
		if err != nil || count <= 0 {
			break // no readers
		}
		if ctx.Err() != nil {
			// Release write lock on timeout
			l.rdb.Do(ctx, l.rdb.B().Del().Key(lockKey).Build())
			return func() {}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Auto-renew
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
		l.rdb.Do(context.Background(), l.rdb.B().Del().Key(lockKey).Build())
	}
}

// RLock acquires one or more shared (read) locks. Keys are sorted internally.
// Returns a single unlock func that releases all acquired locks in reverse order.
func (l *RedisLocker) RLock(ctx context.Context, keys ...string) (unlock func()) {
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
func (l *RedisLocker) rlockOne(ctx context.Context, key string) (unlock func()) {
	lockKey := "lock:" + key
	rlockKey := "rlock:" + key

	// Wait for no active writer
	for {
		exists, _ := l.rdb.Do(ctx, l.rdb.B().Exists().Key(lockKey).Build()).AsBool()
		if !exists {
			break
		}
		if ctx.Err() != nil {
			slog.Warn("rlock: context cancelled", slog.String("key", key))
			return func() {}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Increment reader counter with TTL
	l.rdb.Do(ctx, l.rdb.B().Incr().Key(rlockKey).Build())
	l.rdb.Do(ctx, l.rdb.B().Expire().Key(rlockKey).Seconds(int64(l.cfg.TTL.Seconds())).Build())

	// Auto-renew reader TTL
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
		l.rdb.Do(context.Background(), l.rdb.B().Decr().Key(rlockKey).Build())
	}
}

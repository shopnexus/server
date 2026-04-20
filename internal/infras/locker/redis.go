package locker

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/redis/rueidis"
)

var _ Client = (*RedisLocker)(nil)

// lockScript atomically acquires an exclusive write lock.
// KEYS[1] = lock:<key>, KEYS[2] = rlock:<key> (reader counter)
// ARGV[1] = TTL seconds
// Returns 1 if acquired, 0 if another writer holds the lock or readers are active.
var lockScript = rueidis.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
if tonumber(redis.call('GET', KEYS[2]) or '0') > 0 then return 0 end
redis.call('SET', KEYS[1], '1', 'EX', ARGV[1])
return 1
`)

// rlockScript atomically acquires a shared read lock.
// KEYS[1] = lock:<key>, KEYS[2] = rlock:<key>
// ARGV[1] = TTL seconds
// Returns 1 if acquired, 0 if a writer holds the lock.
var rlockScript = rueidis.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('INCR', KEYS[2])
redis.call('EXPIRE', KEYS[2], ARGV[1])
return 1
`)

// RedisLocker implements distributed RWMutex using Redis with Lua scripts
// for atomic acquire and pipelining for multi-key operations.
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

// Lock acquires one or more exclusive (write) locks. Keys are sorted and acquired
// in a single pipelined round-trip. On partial failure, acquired locks are released
// and the whole batch is retried.
func (l *RedisLocker) Lock(ctx context.Context, keys ...string) (unlock func()) {
	if len(keys) == 0 {
		return func() {}
	}
	sorted := dedupeSort(keys)
	ttlSec := strconv.FormatInt(int64(l.cfg.TTL.Seconds()), 10)

	for {
		acquired := l.execMulti(ctx, lockScript, sorted, ttlSec)
		if len(acquired) == len(sorted) {
			break
		}
		// Partial failure: release acquired write locks and retry
		l.delMany(context.Background(), "lock:", acquired)

		select {
		case <-ctx.Done():
			slog.Warn("lock: context cancelled", slog.Any("keys", sorted))
			return func() {}
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Auto-renewal: pipelined EXPIRE for all keys
	done := make(chan struct{})
	go l.renew(ctx, "lock:", sorted, done)

	return func() {
		close(done)
		l.delMany(context.Background(), "lock:", sorted)
	}
}

// RLock acquires one or more shared (read) locks. Multiple readers may hold
// the same key; blocks while a writer holds the key.
func (l *RedisLocker) RLock(ctx context.Context, keys ...string) (unlock func()) {
	if len(keys) == 0 {
		return func() {}
	}
	sorted := dedupeSort(keys)
	ttlSec := strconv.FormatInt(int64(l.cfg.TTL.Seconds()), 10)

	for {
		acquired := l.execMulti(ctx, rlockScript, sorted, ttlSec)
		if len(acquired) == len(sorted) {
			break
		}
		// Partial failure: decrement acquired reader counters and retry
		l.decrMany(context.Background(), "rlock:", acquired)

		select {
		case <-ctx.Done():
			slog.Warn("rlock: context cancelled", slog.Any("keys", sorted))
			return func() {}
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Auto-renewal: pipelined EXPIRE for reader counters
	done := make(chan struct{})
	go l.renew(ctx, "rlock:", sorted, done)

	return func() {
		close(done)
		l.decrMany(context.Background(), "rlock:", sorted)
	}
}

// execMulti runs the script on all keys in one pipelined round-trip.
// Returns the keys that successfully acquired (script returned 1).
func (l *RedisLocker) execMulti(ctx context.Context, script *rueidis.Lua, keys []string, ttlSec string) []string {
	execs := make([]rueidis.LuaExec, 0, len(keys))
	for _, k := range keys {
		execs = append(execs, rueidis.LuaExec{
			Keys: []string{"lock:" + k, "rlock:" + k},
			Args: []string{ttlSec},
		})
	}
	results := script.ExecMulti(ctx, l.rdb, execs...)

	acquired := make([]string, 0, len(keys))
	for i, r := range results {
		if v, err := r.AsInt64(); err == nil && v == 1 {
			acquired = append(acquired, keys[i])
		}
	}
	return acquired
}

// renew periodically extends the TTL of all keys in a single pipelined batch
// until done is closed or ctx is cancelled.
func (l *RedisLocker) renew(ctx context.Context, prefix string, keys []string, done <-chan struct{}) {
	ticker := time.NewTicker(l.cfg.TTL / 2)
	defer ticker.Stop()

	ttl := int64(l.cfg.TTL.Seconds())
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			cmds := make([]rueidis.Completed, 0, len(keys))
			for _, k := range keys {
				cmds = append(cmds, l.rdb.B().Expire().Key(prefix+k).Seconds(ttl).Build())
			}
			l.rdb.DoMulti(ctx, cmds...)
		}
	}
}

// delMany DELs all prefixed keys in one pipelined round-trip.
func (l *RedisLocker) delMany(ctx context.Context, prefix string, keys []string) {
	if len(keys) == 0 {
		return
	}
	cmds := make([]rueidis.Completed, 0, len(keys))
	for _, k := range keys {
		cmds = append(cmds, l.rdb.B().Del().Key(prefix+k).Build())
	}
	l.rdb.DoMulti(ctx, cmds...)
}

// decrMany DECRs all prefixed reader counters in one pipelined round-trip.
func (l *RedisLocker) decrMany(ctx context.Context, prefix string, keys []string) {
	if len(keys) == 0 {
		return
	}
	cmds := make([]rueidis.Completed, 0, len(keys))
	for _, k := range keys {
		cmds = append(cmds, l.rdb.B().Decr().Key(prefix+k).Build())
	}
	l.rdb.DoMulti(ctx, cmds...)
}

// dedupeSort returns a sorted copy with duplicates removed.
func dedupeSort(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.Strings(out)
	// Dedupe in-place
	n := 0
	for i, k := range out {
		if i == 0 || k != out[n-1] {
			out[n] = k
			n++
		}
	}
	return out[:n]
}

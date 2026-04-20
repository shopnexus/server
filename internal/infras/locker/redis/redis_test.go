package redislocker_test

import (
	"context"
	"shopnexus-server/internal/infras/locker"
	redislocker "shopnexus-server/internal/infras/locker/redis"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/rueidis"
)

func newRealLocker(tb testing.TB) *redislocker.RedisLocker {
	tb.Helper()
	c, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:  []string{"localhost:6379"},
		DisableCache: true,
	})
	if err != nil {
		tb.Fatalf("rueidis connect: %v", err)
	}
	_ = c.Do(context.Background(), c.B().Flushdb().Build())
	return redislocker.NewRedisLocker(c, locker.Config{TTL: 10 * time.Second})
}

func TestRedisLocker_ExclusiveMutex(t *testing.T) {
	lk := newRealLocker(t)
	const workers = 8
	const incsPerWorker = 50

	var counter atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incsPerWorker; j++ {
				unlock := lk.Lock(context.Background(), "counter")
				// Non-atomic read-modify-write inside the critical section —
				// if the lock works, final counter must equal workers*incsPerWorker.
				v := counter.Load()
				time.Sleep(200 * time.Microsecond)
				counter.Store(v + 1)
				unlock()
			}
		}()
	}
	wg.Wait()

	want := int64(workers * incsPerWorker)
	if got := counter.Load(); got != want {
		t.Fatalf("counter = %d, want %d (lock did not serialize)", got, want)
	}
}

func TestRedisLocker_MultiKeyDeadlockFree(t *testing.T) {
	lk := newRealLocker(t)
	// Two goroutines try locks in reverse order — if sorting works, no deadlock.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			unlock := lk.Lock(context.Background(), "a", "b")
			unlock()
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			unlock := lk.Lock(context.Background(), "b", "a")
			unlock()
		}
		done <- struct{}{}
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock: goroutines did not finish within 10s")
		}
	}
}

func TestRedisLocker_RLockParallel(t *testing.T) {
	lk := newRealLocker(t)
	const readers = 5

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := lk.RLock(context.Background(), "shared")
			defer unlock()
			n := concurrent.Add(1)
			// Track peak.
			for {
				m := maxConcurrent.Load()
				if n <= m || maxConcurrent.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrent.Add(-1)
		}()
	}
	wg.Wait()
	if peak := maxConcurrent.Load(); peak < 2 {
		t.Fatalf("RLock did not allow parallel readers (peak=%d)", peak)
	}
}

func TestRedisLocker_WriterWaitsForReaders(t *testing.T) {
	lk := newRealLocker(t)

	rUnlock := lk.RLock(context.Background(), "rw")

	writerAcquired := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		unlock := lk.Lock(context.Background(), "rw")
		writerAcquired <- time.Since(start)
		unlock()
	}()

	// Writer must NOT acquire while reader holds.
	select {
	case <-writerAcquired:
		t.Fatal("writer acquired lock while reader held it")
	case <-time.After(300 * time.Millisecond):
	}

	rUnlock()

	// Writer should acquire within reasonable time after release.
	select {
	case waited := <-writerAcquired:
		if waited > 2*time.Second {
			t.Fatalf("writer took too long: %v", waited)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not acquire after reader released")
	}
}

func TestRedisLocker_ReaderWaitsForWriter(t *testing.T) {
	lk := newRealLocker(t)

	wUnlock := lk.Lock(context.Background(), "rw")

	readerAcquired := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		unlock := lk.RLock(context.Background(), "rw")
		readerAcquired <- time.Since(start)
		unlock()
	}()

	select {
	case <-readerAcquired:
		t.Fatal("reader acquired lock while writer held it")
	case <-time.After(300 * time.Millisecond):
	}

	wUnlock()

	select {
	case waited := <-readerAcquired:
		if waited > 2*time.Second {
			t.Fatalf("reader took too long: %v", waited)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not acquire after writer released")
	}
}

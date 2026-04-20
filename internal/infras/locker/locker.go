package locker

import (
	"context"
	"time"
)

// Client defines the interface for distributed locking mechanisms.
//
// Lock and RLock accept one or more keys. When multiple keys are provided,
// the implementation MUST sort them internally and acquire in deterministic
// order to prevent deadlocks. The returned unlock func releases all acquired
// locks in reverse order.
type Client interface {
	Lock(ctx context.Context, keys ...string) (unlock func())
	RLock(ctx context.Context, keys ...string) (unlock func())
}

type Config struct {
	TTL time.Duration
}

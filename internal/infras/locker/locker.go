package locker

import (
	"context"
	"time"
)

// Client defines the interface for distributed locking mechanisms.
type Client interface {
	Lock(ctx context.Context, key string) (unlock func())
	RLock(ctx context.Context, key string) (unlock func())
}

type Config struct {
	TTL time.Duration
}

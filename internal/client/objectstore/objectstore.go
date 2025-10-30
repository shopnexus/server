package objectstore

import (
	"context"
	"io"
	"time"
)

type Client interface {
	GetURL(ctx context.Context, key string) (string, error)
	GetPresignedURL(ctx context.Context, key string, expireIn time.Duration) (string, error)
	ListObjects(ctx context.Context, prefix string) ([]string, error)
	Upload(ctx context.Context, key string, reader io.Reader, private bool) (string, error)
	Delete(ctx context.Context, key string) error
}

package milvus

import (
	"context"
	"log/slog"

	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// Config holds Milvus connection parameters.
type Config struct {
	Address string `yaml:"address"`
}

// Client wraps the Milvus SDK client.
type Client struct {
	inner  *milvusclient.Client
	config Config
}

// NewClient connects to Milvus and returns a wrapper Client.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	inner, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: cfg.Address,
	})
	if err != nil {
		return nil, err
	}

	slog.Info("connected to Milvus", "address", cfg.Address)
	return &Client{inner: inner, config: cfg}, nil
}

// Close disconnects from Milvus.
func (c *Client) Close(ctx context.Context) error {
	return c.inner.Close(ctx)
}

// Inner returns the underlying SDK client for advanced use.
func (c *Client) Inner() *milvusclient.Client {
	return c.inner
}

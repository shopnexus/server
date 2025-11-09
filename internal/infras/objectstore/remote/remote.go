package remote

import (
	"context"
	"io"
	"time"

	commonmodel "shopnexus-remastered/internal/module/common/model"
)

type ClientImpl struct {
}

type RemoteConfig struct {
}

func NewClient(cfg RemoteConfig) *ClientImpl {
	return &ClientImpl{}
}

func (c *ClientImpl) Config() commonmodel.OptionConfig {
	return commonmodel.OptionConfig{
		ID:          "remote",
		Name:        "Remote Storage",
		Provider:    "Remote",
		Method:      "default",
		Description: "Remote Object Storage",
	}
}

func (c *ClientImpl) GetURL(ctx context.Context, key string) (string, error) {
	return key, nil
}

func (c *ClientImpl) GetPresignedURL(ctx context.Context, key string, _ time.Duration) (string, error) {
	// For local, just return the public URL (no signing).
	return c.GetURL(ctx, key)
}

func (c *ClientImpl) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func (c *ClientImpl) Upload(ctx context.Context, key string, reader io.Reader, private bool) (string, error) {
	return key, nil
}

func (c *ClientImpl) Delete(ctx context.Context, key string) error {
	return nil
}

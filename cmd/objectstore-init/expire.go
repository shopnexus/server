package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/shopnexus/server/internal/platform/config"
)

// expire sets root.Bucket's lifecycle to one rule: an object goes days
// after it was last written. The agent runtime's workspaces use it, so a
// conversation no turn has touched for a week leaves nothing behind. A
// second run changes nothing.
func expire(ctx context.Context, root config.ObjectStoreConfig, days int) error {
	u, err := url.Parse(root.Endpoint)
	if err != nil {
		return fmt.Errorf("OBJECT_STORE_ENDPOINT: %w", err)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(root.AccessKey, root.SecretKey, ""),
		Region: root.Region,
		Secure: u.Scheme == "https",
	})
	if err != nil {
		return fmt.Errorf("object store client: %w", err)
	}
	cfg := lifecycle.NewConfiguration()
	cfg.Rules = []lifecycle.Rule{{
		ID:         "expire",
		Status:     "Enabled",
		RuleFilter: lifecycle.Filter{Prefix: ""},
		Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(days)},
	}}
	if err := client.SetBucketLifecycle(ctx, root.Bucket, cfg); err != nil {
		return fmt.Errorf("bucket %s lifecycle: %w", root.Bucket, err)
	}
	return nil
}

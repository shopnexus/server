package main

import (
	"context"
	"net/url"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
)

// TestExpire sets the test bucket's lifecycle twice and reads one rule back.
// A rule of 7 days removes nothing while the other tests of the bucket run.
func TestExpire(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("needs the object store")
	}
	root, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	objectstoretest.New(t)
	ctx := context.Background()
	for range 2 {
		if err = expire(ctx, root, 7); err != nil {
			t.Fatal(err)
		}
	}
	u, err := url.Parse(root.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(root.AccessKey, root.SecretKey, ""), Region: root.Region, Secure: u.Scheme == "https"})
	if err != nil {
		t.Fatal(err)
	}
	// No rules removes the lifecycle, for the other tests of the bucket.
	t.Cleanup(func() { client.SetBucketLifecycle(ctx, root.Bucket, lifecycle.NewConfiguration()) })
	cfg, err := client.GetBucketLifecycle(ctx, root.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Expiration.Days != 7 || cfg.Rules[0].Status != "Enabled" {
		t.Fatalf("rules %+v", cfg.Rules)
	}
}

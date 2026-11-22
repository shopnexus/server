package main

import (
	"bytes"
	"context"
	"net/url"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
)

// TestGrant gives a user the test bucket and checks it can write there but
// not in another bucket, nor change the bucket's policy.
func TestGrant(t *testing.T) {
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
	u, err := url.Parse(root.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client := func(key, secret string) *minio.Client {
		c, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(key, secret, ""), Region: root.Region, Secure: u.Scheme == "https"})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	other := root.Bucket + "-grant-other"
	if err := client(root.AccessKey, root.SecretKey).MakeBucket(ctx, other, minio.MakeBucketOptions{Region: root.Region}); err != nil {
		if exists, _ := client(root.AccessKey, root.SecretKey).BucketExists(ctx, other); !exists {
			t.Fatal(err)
		}
	}
	key, secret := "grant-test-user", "grant-test-secret-0123456789"
	for range 2 {
		if err := grant(ctx, root, key, secret); err != nil {
			t.Fatal(err)
		}
	}
	user := client(key, secret)
	put := func(bucket string) error {
		_, err := user.PutObject(ctx, bucket, "grant-test.txt", bytes.NewReader([]byte("x")), 1, minio.PutObjectOptions{})
		return err
	}
	if err := put(root.Bucket); err != nil {
		t.Errorf("put in its bucket: %v", err)
	}
	if err := put(other); err == nil {
		t.Error("put in another bucket succeeded")
	}
	if err := user.SetBucketPolicy(ctx, root.Bucket, ""); err == nil {
		t.Error("changing the bucket policy succeeded")
	}
}

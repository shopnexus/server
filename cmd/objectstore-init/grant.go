package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/minio/madmin-go/v4"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/shopnexus/server/internal/platform/config"
)

// grant makes accessKey a user with secretKey that may read, write and list
// root.Bucket and nothing else. root holds the root keys. A second run
// changes nothing, except a new secretKey replaces the old one.
func grant(ctx context.Context, root config.ObjectStoreConfig, accessKey, secretKey string) error {
	u, err := url.Parse(root.Endpoint)
	if err != nil {
		return fmt.Errorf("OBJECT_STORE_ENDPOINT: %w", err)
	}
	admin, err := madmin.NewWithOptions(u.Host, &madmin.Options{
		Creds:  credentials.NewStaticV4(root.AccessKey, root.SecretKey, ""),
		Secure: u.Scheme == "https",
	})
	if err != nil {
		return fmt.Errorf("admin client: %w", err)
	}
	name := "rw-" + root.Bucket
	if err := admin.AddCannedPolicy(ctx, name, bucketPolicy(root.Bucket)); err != nil {
		return fmt.Errorf("policy %s: %w", name, err)
	}
	if err := admin.AddUser(ctx, accessKey, secretKey); err != nil {
		return fmt.Errorf("add: %w", err)
	}
	if _, err := admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{name}, User: accessKey}); err != nil {
		return fmt.Errorf("attach %s: %w", name, err)
	}
	return nil
}

// bucketPolicy allows objects' reads, writes, deletes and multipart uploads
// in bucket, and listing it.
func bucketPolicy(bucket string) []byte {
	return fmt.Appendf(nil, `{"Version":"2012-10-17","Statement":[`+
		`{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:AbortMultipartUpload","s3:ListMultipartUploadParts"],"Resource":["arn:aws:s3:::%[1]s/*"]},`+
		`{"Effect":"Allow","Action":["s3:ListBucket","s3:GetBucketLocation","s3:ListBucketMultipartUploads"],"Resource":["arn:aws:s3:::%[1]s"]}]}`, bucket)
}

// Package objectstore reads and writes objects in the S3-compatible store
// (RustFS). Keys under uploads/ are private and written by clients through
// presigned URLs; keys under media/ are public-read.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/shopnexus/server2/internal/platform/config"
)

var ErrNotFound = errors.New("object not found")

// Store is one bucket of the object store.
type Store struct {
	client *minio.Client
	// presigner signs URLs for the public endpoint; with the region set it
	// never calls the store.
	presigner *minio.Client
	bucket    string
	region    string
	publicURL string
}

func New(cfg config.ObjectStoreConfig) (*Store, error) {
	client, err := clientFor(cfg.Endpoint, cfg)
	if err != nil {
		return nil, err
	}
	presigner, err := clientFor(cfg.PublicEndpoint, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{
		client: client, presigner: presigner, bucket: cfg.Bucket, region: cfg.Region,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}, nil
}

func clientFor(endpoint string, cfg config.ObjectStoreConfig) (*minio.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("object store endpoint %q: want http:// or https:// and a host", endpoint)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("object store client: %w", err)
	}
	return client, nil
}

// Prepare creates the bucket when it is missing and lets anyone read
// media/. cmd/objectstore-init and the test helpers run it.
func (s *Store) Prepare(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
			return fmt.Errorf("create bucket %s: %w", s.bucket, err)
		}
	}
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},` +
		`"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + s.bucket + `/media/*"]}]}`
	if err := s.client.SetBucketPolicy(ctx, s.bucket, policy); err != nil {
		return fmt.Errorf("set bucket policy: %w", err)
	}
	return nil
}

// PresignPut returns a URL and the headers a PUT of exactly size bytes of
// contentType to key must carry, valid for ttl.
func (s *Store) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (putURL string, headers map[string]string, err error) {
	headers = map[string]string{"Content-Type": contentType, "Content-Length": strconv.FormatInt(size, 10)}
	signed := http.Header{}
	for k, v := range headers {
		signed.Set(k, v)
	}
	u, err := s.presigner.PresignHeader(ctx, http.MethodPut, s.bucket, key, ttl, url.Values{}, signed)
	if err != nil {
		return "", nil, fmt.Errorf("presign put %s: %w", key, err)
	}
	return u.String(), headers, nil
}

// Stat returns the size and content type of key, or ErrNotFound.
func (s *Store) Stat(ctx context.Context, key string) (size int64, contentType string, err error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if notFound(err) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("stat %s: %w", key, err)
	}
	return info.Size, info.ContentType, nil
}

// Get opens key for reading; the caller closes it.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	return obj, nil
}

func (s *Store) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	if _, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// Delete removes key; a key that is absent is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil && !notFound(err) {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}

// PublicURL is where a client reads key; only media/ keys are readable.
func (s *Store) PublicURL(key string) string { return s.publicURL + "/" + key }

func notFound(err error) bool {
	if err == nil {
		return false
	}
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NotFound"
}

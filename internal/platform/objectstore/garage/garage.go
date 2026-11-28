// Package garage sets up buckets and keys on Garage, the object store,
// through its admin API (v2). cmd/objectstore-init and the test helpers use
// it; the server itself only speaks S3.
package garage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// Admin calls one Garage's admin API with its admin token.
type Admin struct {
	endpoint string
	token    string
	client   *http.Client
}

// New returns an Admin for the admin API at endpoint (http://host:3903).
func New(endpoint, token string) (*Admin, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("garage admin endpoint %q: want http:// or https:// and a host", endpoint)
	}
	if token == "" {
		return nil, errors.New("garage admin token is empty")
	}
	return &Admin{endpoint: strings.TrimRight(endpoint, "/"), token: token, client: httpx.NewClient(30 * time.Second)}, nil
}

// Bucket is what Ensure makes true of one bucket.
type Bucket struct {
	Name      string // the bucket's global alias
	AccessKey string // a key that may read and write the bucket, nothing else
	SecretKey string
	// Website opens the bucket on Garage's web endpoint, which reads any
	// object without credentials. The media host's nginx sends only media/
	// there.
	Website bool
	// UploadOrigins may PUT to presigned URLs from a browser.
	UploadOrigins []string
	// ExpireDays removes an object that many days after its last write;
	// 0 keeps objects.
	ExpireDays int
}

// ErrKeyTaken is returned when the access key exists with another secret.
// Garage never reuses a key ID, so a new secret needs a new access key.
var ErrKeyTaken = errors.New("the access key exists with another secret; give the new secret a new access key")

// Ensure creates the bucket and the key when missing, lets the key read and
// write the bucket, and sets the bucket's web access, CORS and lifecycle to
// b's. A second run changes nothing.
func (a *Admin) Ensure(ctx context.Context, b Bucket) error {
	var bucket struct {
		ID string `json:"id"`
	}
	err := a.call(ctx, http.MethodGet, "/v2/GetBucketInfo?globalAlias="+url.QueryEscape(b.Name), nil, &bucket)
	if isNotFound(err) {
		err = a.call(ctx, http.MethodPost, "/v2/CreateBucket", map[string]any{"globalAlias": b.Name}, &bucket)
	}
	if err != nil {
		return fmt.Errorf("bucket %s: %w", b.Name, err)
	}

	var key struct {
		SecretAccessKey string `json:"secretAccessKey"` //nolint:tagliatelle // Garage's field names.
	}
	err = a.call(ctx, http.MethodGet, "/v2/GetKeyInfo?showSecretKey=true&id="+url.QueryEscape(b.AccessKey), nil, &key)
	switch {
	case isNotFound(err):
		err = a.call(ctx, http.MethodPost, "/v2/ImportKey", map[string]any{
			"accessKeyId": b.AccessKey, "secretAccessKey": b.SecretKey, "name": b.Name,
		}, nil)
	case err == nil && key.SecretAccessKey != b.SecretKey:
		err = ErrKeyTaken
	default:
	}
	if err != nil {
		return fmt.Errorf("key %s: %w", b.AccessKey, err)
	}

	if err = a.call(ctx, http.MethodPost, "/v2/AllowBucketKey", map[string]any{
		"bucketId": bucket.ID, "accessKeyId": b.AccessKey,
		"permissions": map[string]bool{"read": true, "write": true, "owner": false},
	}, nil); err != nil {
		return fmt.Errorf("allow %s on %s: %w", b.AccessKey, b.Name, err)
	}

	website := map[string]any{"enabled": false}
	if b.Website {
		website = map[string]any{"enabled": true, "indexDocument": "index.html"}
	}
	cors := []map[string]any{}
	if len(b.UploadOrigins) > 0 {
		cors = append(cors, map[string]any{
			"AllowedOrigin": b.UploadOrigins, "AllowedMethod": []string{http.MethodPut},
			"AllowedHeader": []string{"*"}, "MaxAgeSeconds": 3600,
		})
	}
	lifecycle := []map[string]any{}
	if b.ExpireDays > 0 {
		lifecycle = append(lifecycle, map[string]any{
			"ID": "expire", "Status": "Enabled", "Filter": map[string]any{"Prefix": ""},
			"Expiration": map[string]any{"Days": b.ExpireDays},
		})
	}
	if err = a.call(ctx, http.MethodPost, "/v2/UpdateBucket?id="+url.QueryEscape(bucket.ID), map[string]any{
		"websiteAccess": website, "corsRules": cors, "lifecycleRules": lifecycle,
	}, nil); err != nil {
		return fmt.Errorf("bucket %s settings: %w", b.Name, err)
	}
	return nil
}

// Info is the part of a bucket's state the tests check.
//
//nolint:tagliatelle // Garage's field names.
type Info struct {
	WebsiteAccess  bool              `json:"websiteAccess"`
	CORSRules      []json.RawMessage `json:"corsRules"`
	LifecycleRules []json.RawMessage `json:"lifecycleRules"`
	Keys           []struct {
		AccessKeyID string          `json:"accessKeyId"`
		Permissions map[string]bool `json:"permissions"`
	} `json:"keys"`
}

// BucketInfo reads the bucket with global alias name.
func (a *Admin) BucketInfo(ctx context.Context, name string) (Info, error) {
	var info Info
	err := a.call(ctx, http.MethodGet, "/v2/GetBucketInfo?globalAlias="+url.QueryEscape(name), nil, &info)
	return info, err
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string { return fmt.Sprintf("garage admin: %d %s", e.status, e.body) }

func isNotFound(err error) bool {
	var s *statusError
	return errors.As(err, &s) && s.status == http.StatusNotFound
}

func (a *Admin) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader = http.NoBody
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.endpoint+path, body)
	if err != nil {
		return fmt.Errorf("garage admin request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("garage admin %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("garage admin %s: %w", path, err)
	}
	if res.StatusCode != http.StatusOK {
		return &statusError{status: res.StatusCode, body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

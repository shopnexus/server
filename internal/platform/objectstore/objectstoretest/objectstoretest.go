// Package objectstoretest connects tests to the test stack's RustFS.
package objectstoretest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// prepare readies the test stack's bucket once per process.
var prepare = sync.OnceValue(func() error {
	cfg, err := Config()
	if err != nil {
		return err
	}
	s, err := objectstore.New(cfg)
	if err != nil {
		return fmt.Errorf("test object store: %w", err)
	}
	if err = s.Prepare(context.Background()); err != nil {
		return fmt.Errorf("prepare test bucket: %w", err)
	}
	return nil
})

// Config returns the test stack's object store settings, from the
// environment or .env.test.
func Config() (config.ObjectStoreConfig, error) {
	var cfg config.ObjectStoreConfig
	for key, dst := range map[string]*string{
		"TEST_OBJECT_STORE_ENDPOINT":        &cfg.Endpoint,
		"TEST_OBJECT_STORE_PUBLIC_ENDPOINT": &cfg.PublicEndpoint,
		"TEST_OBJECT_STORE_REGION":          &cfg.Region,
		"TEST_OBJECT_STORE_ACCESS_KEY":      &cfg.AccessKey,
		"TEST_OBJECT_STORE_SECRET_KEY":      &cfg.SecretKey,
		"TEST_OBJECT_STORE_BUCKET":          &cfg.Bucket,
		"TEST_MEDIA_PUBLIC_URL":             &cfg.PublicURL,
	} {
		v, err := testenv.Get(key)
		if err != nil {
			return config.ObjectStoreConfig{}, fmt.Errorf("test object store: %w", err)
		}
		*dst = v
	}
	return cfg, nil
}

// New returns a Store on the test stack's bucket, which it prepares once per
// process. Tests write under keys of their own. It skips under -short.
func New(tb testing.TB) *objectstore.Store {
	tb.Helper()
	if testing.Short() {
		tb.Skip("needs the object store")
	}
	cfg, err := Config()
	if err != nil {
		tb.Fatal(err)
	}
	s, err := objectstore.New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	if err := prepare(); err != nil {
		tb.Fatal(err)
	}
	return s
}

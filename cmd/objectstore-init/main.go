// Command objectstore-init creates the OBJECT_STORE_BUCKET bucket when it is
// missing and lets anyone read its media/ keys. make run calls it.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/objectstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	s, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		return fmt.Errorf("object store: %w", err)
	}
	if err := s.Prepare(context.Background()); err != nil {
		return fmt.Errorf("prepare the bucket: %w", err)
	}
	fmt.Println("bucket", cfg.ObjectStore.Bucket, "ready")
	return nil
}

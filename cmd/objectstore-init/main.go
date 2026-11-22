// Command objectstore-init creates the OBJECT_STORE_BUCKET bucket when it is
// missing and lets anyone read its media/ keys. make run calls it.
//
// With RustFS's root keys in OBJECT_STORE_ROOT_ACCESS_KEY and
// OBJECT_STORE_ROOT_SECRET_KEY it works as root and makes
// OBJECT_STORE_ACCESS_KEY a user that may read and write this bucket only.
// Without them OBJECT_STORE_ACCESS_KEY is the root key, as in make run and
// the test stack.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return err
	}
	user := cfg.ObjectStore
	root := user
	rootKey, rootSecret := os.Getenv("OBJECT_STORE_ROOT_ACCESS_KEY"), os.Getenv("OBJECT_STORE_ROOT_SECRET_KEY")
	switch {
	case rootKey == "" && rootSecret == "":
	case rootKey == "" || rootSecret == "":
		return errors.New("set both OBJECT_STORE_ROOT_ACCESS_KEY and OBJECT_STORE_ROOT_SECRET_KEY, or neither")
	case rootKey == user.AccessKey:
		return errors.New("OBJECT_STORE_ACCESS_KEY is the root key: give it a key of its own")
	default:
		root.AccessKey, root.SecretKey = rootKey, rootSecret
	}
	s, err := objectstore.New(root)
	if err != nil {
		return fmt.Errorf("object store: %w", err)
	}
	ctx := context.Background()
	if err := s.Prepare(ctx); err != nil {
		return fmt.Errorf("prepare the bucket: %w", err)
	}
	fmt.Println("bucket", user.Bucket, "ready")
	if rootKey == "" {
		return nil
	}
	if err := grant(ctx, root, user.AccessKey, user.SecretKey); err != nil {
		return fmt.Errorf("user %s: %w", user.AccessKey, err)
	}
	fmt.Println("user", user.AccessKey, "may use bucket", user.Bucket)
	return nil
}

// Command objectstore-init readies one bucket on Garage through its admin
// API: it creates the OBJECT_STORE_BUCKET bucket and the
// OBJECT_STORE_ACCESS_KEY key when missing and lets that key read and write
// that bucket only. make run and the stacks' init jobs call it.
//
// OBJECT_STORE_ADMIN_ENDPOINT and OBJECT_STORE_ADMIN_TOKEN reach the admin
// API. OBJECT_STORE_WEBSITE, true or false, opens the bucket on Garage's
// web endpoint, which the media host's nginx serves media/ from. With
// OBJECT_STORE_EXPIRE_DAYS an object goes that many days after its last
// write; unset or empty, the bucket keeps its objects. WEBSITE_ORIGINS may
// PUT to presigned URLs from a browser.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/objectstore/garage"
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
	var missing []error
	need := func(name string) string {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			missing = append(missing, fmt.Errorf("%s is not set", name))
		}
		return v
	}
	endpoint, token, website := need("OBJECT_STORE_ADMIN_ENDPOINT"), need("OBJECT_STORE_ADMIN_TOKEN"), need("OBJECT_STORE_WEBSITE")
	if err = errors.Join(missing...); err != nil {
		return err
	}
	b := garage.Bucket{
		Name: cfg.ObjectStore.Bucket, AccessKey: cfg.ObjectStore.AccessKey, SecretKey: cfg.ObjectStore.SecretKey,
		UploadOrigins: cfg.ObjectStore.UploadOrigins,
	}
	if b.Website, err = strconv.ParseBool(website); err != nil {
		return fmt.Errorf("OBJECT_STORE_WEBSITE %q: want true or false", website)
	}
	if v := os.Getenv("OBJECT_STORE_EXPIRE_DAYS"); v != "" {
		if b.ExpireDays, err = strconv.Atoi(v); err != nil || b.ExpireDays < 1 {
			return fmt.Errorf("OBJECT_STORE_EXPIRE_DAYS %q: want a number of days, 1 or more", v)
		}
	}
	admin, err := garage.New(endpoint, token)
	if err != nil {
		return fmt.Errorf("object store admin: %w", err)
	}
	if err = admin.Ensure(context.Background(), b); err != nil {
		return fmt.Errorf("object store: %w", err)
	}
	fmt.Printf("bucket %s ready for key %s (website %t, expire days %d)\n", b.Name, b.AccessKey, b.Website, b.ExpireDays)
	return nil
}

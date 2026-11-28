package garage_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"

	"github.com/shopnexus/server/internal/platform/objectstore/garage"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
)

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// newBucket returns a bucket of the test's own, with a fresh key.
func newBucket(t *testing.T) (*garage.Admin, garage.Bucket) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the object store")
	}
	admin, err := objectstoretest.Admin()
	if err != nil {
		t.Fatal(err)
	}
	return admin, garage.Bucket{
		Name: "garage-test-" + randHex(t, 6), AccessKey: "GK" + randHex(t, 12), SecretKey: randHex(t, 32),
	}
}

// Ensure twice leaves one key with read and write, web access, one CORS
// rule and one lifecycle rule; a run without them removes them.
func TestEnsure(t *testing.T) {
	t.Parallel()
	admin, b := newBucket(t)
	b.Website, b.UploadOrigins, b.ExpireDays = true, []string{"https://shop.example.test"}, 7
	for range 2 {
		if err := admin.Ensure(t.Context(), b); err != nil {
			t.Fatal(err)
		}
	}
	info, err := admin.BucketInfo(t.Context(), b.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !info.WebsiteAccess || len(info.CORSRules) != 1 || len(info.LifecycleRules) != 1 {
		t.Fatalf("settings %+v", info)
	}
	if len(info.Keys) != 1 || info.Keys[0].AccessKeyID != b.AccessKey ||
		!info.Keys[0].Permissions["read"] || !info.Keys[0].Permissions["write"] || info.Keys[0].Permissions["owner"] {
		t.Fatalf("keys %+v", info.Keys)
	}

	b.Website, b.UploadOrigins, b.ExpireDays = false, nil, 0
	if err = admin.Ensure(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	if info, err = admin.BucketInfo(t.Context(), b.Name); err != nil {
		t.Fatal(err)
	}
	if info.WebsiteAccess || len(info.CORSRules) != 0 || len(info.LifecycleRules) != 0 {
		t.Fatalf("settings after removing them %+v", info)
	}
}

// Garage never reuses a key ID, so a new secret on an existing key fails.
func TestEnsureRefusesANewSecret(t *testing.T) {
	t.Parallel()
	admin, b := newBucket(t)
	if err := admin.Ensure(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	b.SecretKey = randHex(t, 32)
	if err := admin.Ensure(t.Context(), b); !errors.Is(err, garage.ErrKeyTaken) {
		t.Fatalf("got %v", err)
	}
}

// The upload origins may PUT from a browser; no other origin may.
func TestEnsureAllowsTheWebsiteToPut(t *testing.T) {
	t.Parallel()
	admin, b := newBucket(t)
	b.UploadOrigins = []string{"https://shop.example.test"}
	if err := admin.Ensure(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	cfg, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	preflight := func(origin string) (int, string) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodOptions, cfg.Endpoint+"/"+b.Name+"/uploads/x", http.NoBody)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPut)
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode, res.Header.Get("Access-Control-Allow-Origin")
	}
	if status, got := preflight("https://shop.example.test"); status != http.StatusOK || got != "https://shop.example.test" {
		t.Fatalf("the website's preflight: %d, Access-Control-Allow-Origin %q", status, got)
	}
	// A browser drops a preflight that is not 2xx, whatever its headers.
	if status, _ := preflight("https://evil.example.test"); status < 300 {
		t.Fatalf("another origin's preflight: %d", status)
	}
}

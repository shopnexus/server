package objectstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
)

func put(t *testing.T, url string, headers map[string]string, body []byte) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.ContentLength = int64(len(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func get(t *testing.T, url string) (status int, body []byte) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestPresignedPutThenReadAndDelete(t *testing.T) {
	t.Parallel()
	s := objectstoretest.New(t)
	ctx := context.Background()
	key, body := "uploads/"+uuid.NewV7().String(), []byte("a photo's bytes")
	url, headers, err := s.PresignPut(ctx, key, "image/jpeg", int64(len(body)), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if code := put(t, url, headers, body); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	size, contentType, err := s.Stat(ctx, key)
	if err != nil || size != int64(len(body)) || contentType != "image/jpeg" {
		t.Fatalf("stat: %d %q %v", size, contentType, err)
	}
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("get: %q", got)
	}
	for range 2 {
		if err := s.Delete(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Stat(ctx, key); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("stat after delete: %v", err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}

// The presigned request fixes the length: a PUT of other bytes is refused.
func TestPresignedPutFixesTheLength(t *testing.T) {
	t.Parallel()
	s := objectstoretest.New(t)
	key := "uploads/" + uuid.NewV7().String()
	url, headers, err := s.PresignPut(context.Background(), key, "image/png", 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	headers["Content-Length"] = strconv.Itoa(6)
	if code := put(t, url, headers, []byte("sixbyt")); code == http.StatusOK {
		t.Fatal("a PUT longer than presigned was accepted")
	}
}

// media/ is readable without credentials; uploads/ is not.
func TestOnlyMediaIsPublic(t *testing.T) {
	t.Parallel()
	s := objectstoretest.New(t)
	ctx := context.Background()
	id := uuid.NewV7().String()
	for _, key := range []string{"media/" + id, "uploads/" + id} {
		if err := s.Put(ctx, key, "image/jpeg", bytes.NewReader([]byte("x")), 1); err != nil {
			t.Fatal(err)
		}
	}
	if code, body := get(t, s.PublicURL("media/"+id)); code != http.StatusOK || string(body) != "x" {
		t.Fatalf("media: %d %q", code, body)
	}
	if code, _ := get(t, s.PublicURL("uploads/"+id)); code == http.StatusOK {
		t.Fatal("uploads/ is public")
	}
}

// A presigned PUT names the endpoint clients reach, which signs its host,
// while the server talks to the store at its own endpoint.
func TestPresignsForThePublicEndpoint(t *testing.T) {
	t.Parallel()
	cfg, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PublicEndpoint = "https://photos.example.test"
	s, err := objectstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.PresignPut(context.Background(), "uploads/"+uuid.NewV7().String(), "image/jpeg", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "https://photos.example.test/") {
		t.Fatalf("%s", u)
	}
}

// A presigned GET reads a private object until its TTL, through the public
// endpoint, without credentials.
func TestPresignGet(t *testing.T) {
	t.Parallel()
	s := objectstoretest.New(t)
	ctx := context.Background()
	key, body := "uploads/"+uuid.NewV7().String(), []byte("a frame's bytes")
	if err := s.Put(ctx, key, "image/jpeg", bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	url, err := s.PresignGet(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if status, got := get(t, url); status != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("presigned get: %d %q", status, got)
	}
	if status, _ := get(t, strings.Replace(url, key, "uploads/"+uuid.NewV7().String(), 1)); status == http.StatusOK {
		t.Fatal("the signature covers the key")
	}
}

// Prepare lets the website's origins PUT to presigned URLs from a browser,
// and no other origin.
func TestPrepareAllowsTheWebsiteToPut(t *testing.T) {
	t.Parallel()
	cfg, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("needs the object store")
	}
	cfg.UploadOrigins = []string{"https://shop.example.test"}
	s, err := objectstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, _, err := s.PresignPut(t.Context(), "uploads/"+uuid.NewV7().String(), "image/jpeg", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	preflight := func(origin string) string {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodOptions, u, http.NoBody)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPut)
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.Header.Get("Access-Control-Allow-Origin")
	}
	if got := preflight("https://shop.example.test"); got != "https://shop.example.test" {
		t.Fatalf("the website's preflight: Access-Control-Allow-Origin %q", got)
	}
	if got := preflight("https://evil.example.test"); got != "" {
		t.Fatalf("another origin's preflight: Access-Control-Allow-Origin %q", got)
	}
}

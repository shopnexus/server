package firebase

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var start = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// server serves body with cacheControl, or status when it is not 200, and
// counts requests.
type server struct {
	mu           sync.Mutex
	status       int
	body         string
	cacheControl string
	hits         atomic.Int32
}

func (s *server) set(status int, body, cacheControl string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body, s.cacheControl = status, body, cacheControl
}

func (s *server) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.hits.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheControl != "" {
		w.Header().Set("Cache-Control", s.cacheControl)
	}
	w.WriteHeader(s.status)
	_, _ = w.Write([]byte(s.body))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func ecJWK(t *testing.T, kid string) (map[string]string, *ecdsa.PublicKey) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"kty": "EC", "crv": "P-256", "kid": kid, "use": "sig", "x": b64(pt[1:33]), "y": b64(pt[33:])}, &k.PublicKey
}

func rsaJWK(t *testing.T, kid string, bits int) (map[string]string, *rsa.PublicKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"kty": "RSA", "kid": kid, "use": "sig", "n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
	}, &k.PublicKey
}

func doc(t *testing.T, keys ...map[string]string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newSet is a key set on srv with its clock at *at.
func newSet(t *testing.T, srv *server, at *time.Time) *keySet {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &keySet{url: ts.URL, http: ts.Client(), now: func() time.Time { return *at }, log: slog.New(slog.DiscardHandler)}
}

func TestKeySetReadsECAndRSAKeys(t *testing.T) {
	t.Parallel()
	ec, ecPub := ecJWK(t, "ec")
	rs, rsaPub := rsaJWK(t, "rsa", 2048)
	small, _ := rsaJWK(t, "small", 1024)
	enc, _ := ecJWK(t, "enc")
	enc["use"] = "enc"
	offCurve, _ := ecJWK(t, "off")
	offCurve["y"] = b64(make([]byte, 32))
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec, rs, small, enc, offCurve, map[string]string{"kty": "oct", "kid": "oct", "k": "AAAA"}), "")
	at := start
	s := newSet(t, srv, &at)
	ctx := context.Background()
	if k, err := s.key(ctx, "ec"); err != nil || !k.(*ecdsa.PublicKey).Equal(ecPub) {
		t.Fatalf("ec: %v", err)
	}
	if k, err := s.key(ctx, "rsa"); err != nil || !k.(*rsa.PublicKey).Equal(rsaPub) {
		t.Fatalf("rsa: %v", err)
	}
	for _, kid := range []string{"small", "enc", "off", "oct"} {
		if _, err := s.key(ctx, kid); !errors.Is(err, errUnknownKey) {
			t.Errorf("%s is held: %v", kid, err)
		}
	}
}

func TestKeySetCachesForMaxAge(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "public, max-age=3600, must-revalidate")
	at := start
	s := newSet(t, srv, &at)
	for range 3 {
		if _, err := s.key(context.Background(), "ec"); err != nil {
			t.Fatal(err)
		}
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetches within max-age: %d", n)
	}
	at = start.Add(time.Hour)
	if _, err := s.key(context.Background(), "ec"); err != nil || srv.hits.Load() != 2 {
		t.Fatalf("after max-age: %v, %d fetches", err, srv.hits.Load())
	}
}

// An unknown kid fetches again, at most once a minute, so forged kids
// cannot make the server call Google in a loop.
func TestKeySetUnknownKidFetchesOnceAMinute(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "")
	at := start
	s := newSet(t, srv, &at)
	ctx := context.Background()
	if _, err := s.key(ctx, "ec"); err != nil {
		t.Fatal(err)
	}
	for _, kid := range []string{"forged-1", "forged-2"} {
		if _, err := s.key(ctx, kid); !errors.Is(err, errUnknownKey) {
			t.Fatalf("%s: %v", kid, err)
		}
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("unknown kids within a minute fetched: %d", n)
	}
	rotated, _ := ecJWK(t, "rotated")
	srv.set(http.StatusOK, doc(t, ec, rotated), "")
	at = start.Add(time.Minute)
	if _, err := s.key(ctx, "rotated"); err != nil || srv.hits.Load() != 2 {
		t.Fatalf("a key published since: %v, %d fetches", err, srv.hits.Load())
	}
}

func TestKeySetFailedFetchKeepsTheKeys(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "max-age=60")
	at := start
	s := newSet(t, srv, &at)
	if _, err := s.key(context.Background(), "ec"); err != nil {
		t.Fatal(err)
	}
	srv.set(http.StatusServiceUnavailable, "down", "")
	at = start.Add(2 * time.Minute)
	if _, err := s.key(context.Background(), "ec"); err != nil || srv.hits.Load() != 2 {
		t.Fatalf("a failed refresh: %v, %d fetches", err, srv.hits.Load())
	}
}

func TestKeySetBadBodyKeepsTheKeys(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "max-age=60")
	at := start
	s := newSet(t, srv, &at)
	if _, err := s.key(context.Background(), "ec"); err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"<html>", `{"keys": [` + strings.Repeat(`{"kty":"EC"},`, 1<<17) + `{}]}`, `{}`, `{"keys": []}`, `null`} {
		srv.set(http.StatusOK, body, "")
		at = start.Add(time.Duration(i+2) * time.Minute)
		if _, err := s.key(context.Background(), "ec"); err != nil {
			t.Fatalf("body %d replaced the keys: %v", i, err)
		}
	}
}

func TestKeySetWithoutKeysIsUnavailable(t *testing.T) {
	t.Parallel()
	srv := &server{}
	srv.set(http.StatusInternalServerError, "down", "")
	at := start
	s := newSet(t, srv, &at)
	for range 2 {
		if _, err := s.key(context.Background(), "ec"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%v", err)
		}
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetches within a minute while down: %d", n)
	}
}

func TestKeySetParallelLookupsFetchOnce(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "max-age=60")
	at := start
	s := newSet(t, srv, &at)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.key(context.Background(), "ec"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("parallel lookups fetched %d times", n)
	}
}

// A caller that hangs up during the first fetch must not leave the key set
// empty for the minute that follows.
func TestKeySetCancelledCallerStillFetches(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "max-age=60")
	at := start
	s := newSet(t, srv, &at)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = s.key(gone, "ec")
	if _, err := s.key(context.Background(), "ec"); err != nil {
		t.Fatalf("after a cancelled caller: %v", err)
	}
}

// While the key URL is down, a kid the held keys lack may be a key Google
// rotated in, so it is unavailable, not invalid.
func TestKeySetUnknownKidWhileDownIsUnavailable(t *testing.T) {
	t.Parallel()
	ec, _ := ecJWK(t, "ec")
	srv := &server{}
	srv.set(http.StatusOK, doc(t, ec), "max-age=60")
	at := start
	s := newSet(t, srv, &at)
	if _, err := s.key(context.Background(), "ec"); err != nil {
		t.Fatal(err)
	}
	srv.set(http.StatusServiceUnavailable, "down", "")
	at = start.Add(2 * time.Minute)
	for _, kid := range []string{"rotated", "rotated"} {
		if _, err := s.key(context.Background(), kid); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s while down: %v", kid, err)
		}
	}
	if _, err := s.key(context.Background(), "ec"); err != nil {
		t.Fatalf("a held key while down: %v", err)
	}
}

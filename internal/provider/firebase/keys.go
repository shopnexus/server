// Package firebase checks the phone tokens Firebase issues: ID tokens of
// Firebase Authentication after an SMS code, and Phone Number Verification
// tokens from the SIM.
package firebase

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

const (
	// refetchGap is the least time between two fetches of one key set.
	refetchGap   = time.Minute
	fetchTimeout = 5 * time.Second
	maxJWKS      = 1 << 20
)

// ErrUnavailable is a token whose keys could not be fetched yet. The app
// tries again instead of asking for a new code.
var ErrUnavailable = apperr.New(apperr.KindUnavailable, "unavailable", "phone tokens cannot be checked right now; try again")

var errUnknownKey = errors.New("firebase: no key with this kid")

// keySet holds the public keys published at one JWKS URL. Lookups share one
// mutex, so requests that arrive during a fetch wait for it.
type keySet struct {
	url  string
	http *http.Client
	now  func() time.Time
	log  *slog.Logger

	mu        sync.Mutex
	keys      map[string]crypto.PublicKey // nil until a fetch succeeds
	freshTill time.Time                   // zero when the response gave no max-age
	tried     time.Time                   // the last fetch, successful or not
}

// key returns the key named kid. It fetches when no keys are held, when
// max-age has passed or when kid is unknown, at most once a minute. A failed
// fetch keeps the keys held.
func (s *keySet) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	k, held := s.keys[kid]
	expired := !s.freshTill.IsZero() && !now.Before(s.freshTill)
	if (!held || expired) && (s.tried.IsZero() || now.Sub(s.tried) >= refetchGap) {
		s.tried = now
		keys, freshTill, err := s.fetch(ctx)
		if err != nil {
			s.log.WarnContext(ctx, "fetch phone token keys", "url", s.url, "err", err)
		} else {
			s.keys, s.freshTill = keys, freshTill
			k, held = s.keys[kid]
		}
	}
	switch {
	case s.keys == nil:
		return nil, ErrUnavailable
	case !held:
		return nil, errUnknownKey
	default:
		return k, nil
	}
}

// fetch reads the JWKS document and the time its keys stay fresh until.
func (s *keySet) fetch(ctx context.Context) (map[string]crypto.PublicKey, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, http.NoBody)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("build request: %w", err)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get keys: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("get keys: status %d", res.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxJWKS)).Decode(&doc); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode keys: %w", err)
	}
	keys := make(map[string]crypto.PublicKey, len(doc.Keys))
	for i := range doc.Keys {
		if pub := doc.Keys[i].public(); pub != nil && doc.Keys[i].Kid != "" {
			keys[doc.Keys[i].Kid] = pub
		}
	}
	var freshTill time.Time
	for d := range strings.SplitSeq(res.Header.Get("Cache-Control"), ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(d), "max-age="); ok {
			if secs, convErr := strconv.Atoi(v); convErr == nil && secs >= 0 {
				freshTill = s.now().Add(time.Duration(secs) * time.Second)
			}
		}
	}
	return keys, freshTill, nil
}

// jwk is one key of a JWKS document, as RFC 7517 writes it.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// public is the signing key j describes: a P-256 point on the curve, or an
// RSA key of at least 2048 bits. Anything else is nil.
func (j jwk) public() crypto.PublicKey {
	if j.Use != "" && j.Use != "sig" {
		return nil
	}
	switch j.Kty {
	case "EC":
		x, errX := base64.RawURLEncoding.DecodeString(j.X)
		y, errY := base64.RawURLEncoding.DecodeString(j.Y)
		if j.Crv != "P-256" || errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return nil
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), slices.Concat([]byte{4}, x, y))
		if err != nil {
			return nil
		}
		return pub
	case "RSA":
		n, errN := base64.RawURLEncoding.DecodeString(j.N)
		e, errE := base64.RawURLEncoding.DecodeString(j.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			return nil
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if pub.N.BitLen() < 2048 || pub.E < 3 {
			return nil
		}
		return pub
	default:
		return nil
	}
}

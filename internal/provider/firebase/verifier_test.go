package firebase_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/provider/firebase"
	"github.com/shopnexus/server2/internal/provider/firebase/firebasetest"
)

var at = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

const phone = "+84901234567"

func verifier(i *firebasetest.Issuer, testNumbers bool) *firebase.PhoneVerifier {
	return firebase.NewPhoneVerifier(i.Config(testNumbers), func() time.Time { return at }, slog.New(slog.DiscardHandler))
}

func newIssuer(t *testing.T) *firebasetest.Issuer {
	t.Helper()
	i := firebasetest.New(t)
	i.Now = func() time.Time { return at }
	return i
}

func TestVerifyAcceptsBothIssuers(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	v := verifier(i, false)
	for name, token := range map[string]string{
		"sms":             i.SMSToken(phone),
		"sim":             i.PNVToken(phone),
		"iat 20 s ahead":  i.SMSToken(phone, firebasetest.Claim("iat", at.Add(20*time.Second).Unix())),
		"verified 4m ago": i.SMSToken(phone, firebasetest.Claim("auth_time", at.Add(-4*time.Minute).Unix())),
	} {
		if got, err := v.Verify(context.Background(), token); err != nil || got.Value() != phone {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}

func TestVerifyRefuses(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	v := verifier(i, false)
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"not a jwt":         "abc.def",
		"unknown issuer":    i.SMSToken(phone, firebasetest.Claim("iss", "https://securetoken.google.com/other")),
		"wrong audience":    i.SMSToken(phone, firebasetest.Claim("aud", "other")),
		"expired":           i.SMSToken(phone, firebasetest.Claim("exp", at.Add(-time.Minute).Unix())),
		"no exp":            i.SMSToken(phone, firebasetest.Drop("exp")),
		"iat 60 s ahead":    i.SMSToken(phone, firebasetest.Claim("iat", at.Add(time.Minute).Unix())),
		"verified 6m ago":   i.SMSToken(phone, firebasetest.Claim("auth_time", at.Add(-6*time.Minute).Unix())),
		"no auth_time":      i.SMSToken(phone, firebasetest.Drop("auth_time")),
		"another provider":  i.SMSToken(phone, firebasetest.Claim("firebase", map[string]any{"sign_in_provider": "password"})),
		"no phone_number":   i.SMSToken(phone, firebasetest.Drop("phone_number")),
		"signed by another": i.SMSToken(phone, firebasetest.SignedWith(jwt.SigningMethodRS256, stranger)),
		"unknown kid":       i.SMSToken(phone, firebasetest.Header("kid", "rsa-9")),
		"sim typ":           i.PNVToken(phone, firebasetest.Header("typ", "at+jwt")),
		"sim audience":      i.PNVToken(phone, firebasetest.Claim("aud", []string{"https://fpnv.googleapis.com/projects/other"})),
		"sim issued 6m ago": i.PNVToken(phone, firebasetest.Claim("iat", at.Add(-6*time.Minute).Unix())),
		"sim no iat":        i.PNVToken(phone, firebasetest.Drop("iat")),
		"sim test number":   i.PNVToken("+840000000000", firebasetest.Claim("test_number", true)),
	} {
		if _, err := v.Verify(context.Background(), token); !errors.Is(err, firebase.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A token must use its issuer's algorithm, so none of these verify: alg
// none, HS256 keyed with the public key, and each issuer's token signed with
// the other's algorithm.
func TestVerifyRefusesAlgorithmSwaps(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	v := verifier(i, false)
	pub, err := x509.MarshalPKIXPublicKey(&mustRSA(t).PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"none":         i.SMSToken(phone, firebasetest.SignedWith(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType)),
		"hs256":        i.SMSToken(phone, firebasetest.SignedWith(jwt.SigningMethodHS256, pub)),
		"sms as es256": i.SMSToken(phone, firebasetest.SignedWith(jwt.SigningMethodES256, mustEC(t))),
		"sim as rs256": i.PNVToken(phone, firebasetest.SignedWith(jwt.SigningMethodRS256, mustRSA(t))),
	} {
		if _, err := v.Verify(context.Background(), token); !errors.Is(err, firebase.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifyTestNumbersWhenAllowed(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	got, err := verifier(i, true).Verify(context.Background(), i.PNVToken("+840000000000", firebasetest.Claim("test_number", true)))
	if err != nil || got.Value() != "+840000000000" || got.Kind() != contact.Phone {
		t.Fatalf("%v %v", got, err)
	}
}

func TestVerifyForeignNumber(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	if _, err := verifier(i, false).Verify(context.Background(), i.SMSToken("+14155550100")); !errors.Is(err, contact.ErrInvalidPhone) {
		t.Fatalf("%v", err)
	}
}

func TestVerifyWithoutKeysIsUnavailable(t *testing.T) {
	t.Parallel()
	i := newIssuer(t)
	i.SetPNVDown(true)
	if _, err := verifier(i, false).Verify(context.Background(), i.PNVToken(phone)); !errors.Is(err, firebase.ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}

func mustRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustEC(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

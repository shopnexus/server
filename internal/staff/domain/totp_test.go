package domain_test

import (
	"bytes"
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/staff/domain"
)

// RFC 6238 appendix B, SHA-1 rows, cut to six digits.
func TestCodeRFC6238(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := domain.Code(secret, time.Unix(unix, 0)); got != want {
			t.Errorf("T=%d: got %s, want %s", unix, got, want)
		}
	}
}

func TestTOTPURI(t *testing.T) {
	t.Parallel()
	secret := domain.NewTOTPSecret()
	u, err := url.Parse(domain.TOTPURI(secret, "mod@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(u.Query().Get("secret"))
	if u.Scheme != "otpauth" || u.Host != "totp" || !strings.HasSuffix(u.Path, "shopnexus staff:mod@example.com") ||
		u.Query().Get("issuer") != "shopnexus staff" || !bytes.Equal(got, secret) || len(secret) != 20 {
		t.Fatalf("%s", u)
	}
	if domain.EncodeSecret(secret) != u.Query().Get("secret") {
		t.Fatal("EncodeSecret differs from the URI's secret")
	}
}

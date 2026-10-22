package otp_test

import (
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var now = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func challenge(t *testing.T) otp.Challenge {
	t.Helper()
	phone, _ := contact.ParsePhone("0901234567")
	return otp.New(uuid.NewV7(), otp.PurposeLogin, phone, netip.MustParseAddr("203.0.113.7"), []byte("right"), now)
}

func TestNewChallenge(t *testing.T) {
	t.Parallel()
	c := challenge(t)
	if !c.ExpiresAt().Equal(now.Add(otp.CodeTTL)) || c.Attempts() != 0 || c.ConsumedAt() != nil || c.WriteID() != c.ID() {
		t.Fatalf("%+v", c)
	}
}

func TestVerifyRightCodeConsumes(t *testing.T) {
	t.Parallel()
	c := challenge(t)
	if err := c.Verify([]byte("right"), now.Add(time.Minute)); err != nil || c.ConsumedAt() == nil {
		t.Fatalf("%v %v", err, c.ConsumedAt())
	}
	if err := c.Verify([]byte("right"), now.Add(time.Minute)); !errors.Is(err, otp.ErrCodeExpired) {
		t.Fatalf("second use: %v", err)
	}
}

func TestVerifyWrongCodeCountsAndLocks(t *testing.T) {
	t.Parallel()
	c := challenge(t)
	err := c.Verify([]byte("wrong"), now)
	var ae *apperr.Error
	if !errors.Is(err, otp.ErrInvalidCode) || !errors.As(err, &ae) || ae.Details["attempts_left"] != otp.MaxAttempts-1 {
		t.Fatalf("err %v", err)
	}
	for i := 2; i < otp.MaxAttempts; i++ {
		c.Verify([]byte("wrong"), now)
	}
	if err := c.Verify([]byte("wrong"), now); !errors.Is(err, otp.ErrCodeLocked) {
		t.Fatalf("last attempt: %v", err)
	}
	if err := c.Verify([]byte("right"), now); !errors.Is(err, otp.ErrCodeLocked) || c.Attempts() != otp.MaxAttempts {
		t.Fatalf("right code after lock: %v, attempts %d", err, c.Attempts())
	}
}

func TestVerifyExpired(t *testing.T) {
	t.Parallel()
	c := challenge(t)
	if err := c.Verify([]byte("wrong"), now.Add(otp.CodeTTL)); !errors.Is(err, otp.ErrCodeExpired) || c.Attempts() != 0 {
		t.Fatalf("%v, attempts %d", err, c.Attempts())
	}
}

// A Restate step journals the challenge it loaded; the copy read back must be
// the same challenge.
func TestChallengeJSONRoundTrip(t *testing.T) {
	t.Parallel()
	c := challenge(t)
	c.Verify([]byte("wrong"), now)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back otp.Challenge
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, c) {
		t.Fatalf("got %+v, want %+v", back, c)
	}
}

package auth_test

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/otp/otptest"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
)

func phone(t *testing.T, raw string) contact.Address {
	t.Helper()
	a, err := contact.ParsePhone(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdmitSpacingAndHourlyCap(t *testing.T) {
	q, dest, ctx := otptest.NewQuotas(), phone(t, "0901234567"), context.Background()
	at := now
	for i := range 5 {
		if err := auth.AdmitOTP(ctx, q, dest, clientIP, at); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
		err := auth.AdmitOTP(ctx, q, dest, clientIP, at.Add(44*time.Second))
		var ae *apperr.Error
		if !errors.Is(err, auth.ErrOTPTooSoon) || !errors.As(err, &ae) || ae.Details["retry_after"] != 1 {
			t.Fatalf("at 44 s: %v", err)
		}
		at = at.Add(45 * time.Second)
	}
	if err := auth.AdmitOTP(ctx, q, dest, clientIP, at); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("sixth send: %v", err)
	}
}

func TestAdmitCapsOneAddress(t *testing.T) {
	q, ctx := otptest.NewQuotas(), context.Background()
	for i := range 20 {
		if err := auth.AdmitOTP(ctx, q, phone(t, fmt.Sprintf("09%08d", i)), clientIP, now); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := auth.AdmitOTP(ctx, q, phone(t, "0987654321"), clientIP, now); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("21st: %v", err)
	}
	if err := auth.AdmitOTP(ctx, q, phone(t, "0987654322"), netip.MustParseAddr("198.51.100.1"), now); err != nil {
		t.Fatalf("another address: %v", err)
	}
}

// losing makes the first n saves lose a race.
type losing struct {
	otp.QuotaStore
	n int
}

func (l *losing) Save(ctx context.Context, q *otp.Quota, w uuid.UUID) error {
	if l.n > 0 {
		l.n--
		return otp.ErrConflict
	}
	return l.QuotaStore.Save(ctx, q, w)
}

func TestAdmitRetriesConflicts(t *testing.T) {
	ctx := context.Background()
	if err := auth.AdmitOTP(ctx, &losing{otptest.NewQuotas(), 2}, phone(t, "0901234567"), clientIP, now); err != nil {
		t.Fatalf("two conflicts: %v", err)
	}
	if err := auth.AdmitOTP(ctx, &losing{otptest.NewQuotas(), 3}, phone(t, "0901234567"), clientIP, now); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("three conflicts: %v", err)
	}
}

func TestNewChallengeHashesTheDerivedCode(t *testing.T) {
	id, dest := uuid.NewV7(), phone(t, "0901234567")
	c := auth.NewChallenge(codes(), id, otp.PurposeLogin, dest, clientIP, now)
	if c.ID() != id || !hmac.Equal(c.CodeHash(), codes().Hash(dest, codes().For(id))) {
		t.Fatalf("%+v", c)
	}
}

func TestSendCode(t *testing.T) {
	ctx, b := context.Background(), &inbox{}
	email, _ := contact.ParseEmail("linh@gmail.com")
	for _, dest := range []contact.Address{phone(t, "0901234567"), email} {
		c := auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, dest, clientIP, now)
		if err := auth.SendCode(ctx, b, emailInbox{b}, codes(), c, now); err != nil {
			t.Fatal(err)
		}
		last := b.all()[len(b.all())-1]
		if last.To != dest.Value() || !strings.Contains(last.Text, codes().For(c.ID())) {
			t.Fatalf("%+v", last)
		}
	}
	expired := auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, phone(t, "0901234567"), clientIP, now)
	if err := auth.SendCode(ctx, b, emailInbox{b}, codes(), expired, now.Add(otp.CodeTTL)); err != nil || len(b.all()) != 2 {
		t.Fatalf("expired challenge sent: %v, %d messages", err, len(b.all()))
	}
}

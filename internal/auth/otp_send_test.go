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
		if err := auth.AdmitOTP(ctx, q, dest, clientIP, at, uuid.NewV7()); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
		err := auth.AdmitOTP(ctx, q, dest, clientIP, at.Add(44*time.Second), uuid.NewV7())
		var ae *apperr.Error
		if !errors.Is(err, auth.ErrOTPTooSoon) || !errors.As(err, &ae) || ae.Details["retry_after"] != 1 {
			t.Fatalf("at 44 s: %v", err)
		}
		at = at.Add(45 * time.Second)
	}
	if err := auth.AdmitOTP(ctx, q, dest, clientIP, at, uuid.NewV7()); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("sixth send: %v", err)
	}
}

func TestAdmitCapsOneAddress(t *testing.T) {
	q, ctx := otptest.NewQuotas(), context.Background()
	for i := range 20 {
		if err := auth.AdmitOTP(ctx, q, phone(t, fmt.Sprintf("09%08d", i)), clientIP, now, uuid.NewV7()); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := auth.AdmitOTP(ctx, q, phone(t, "0987654321"), clientIP, now, uuid.NewV7()); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("21st: %v", err)
	}
	if err := auth.AdmitOTP(ctx, q, phone(t, "0987654322"), netip.MustParseAddr("198.51.100.1"), now, uuid.NewV7()); err != nil {
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
	if err := auth.AdmitOTP(ctx, &losing{otptest.NewQuotas(), 2}, phone(t, "0901234567"), clientIP, now, uuid.NewV7()); err != nil {
		t.Fatalf("two conflicts: %v", err)
	}
	if err := auth.AdmitOTP(ctx, &losing{otptest.NewQuotas(), 3}, phone(t, "0901234567"), clientIP, now, uuid.NewV7()); !errors.Is(err, auth.ErrOTPLimit) {
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
		if sent, err := auth.SendCode(ctx, b, emailInbox{b}, codes(), c, now); err != nil || !sent {
			t.Fatal(sent, err)
		}
		last := b.all()[len(b.all())-1]
		if last.To != dest.Value() || !strings.Contains(last.Text, codes().For(c.ID())) {
			t.Fatalf("%+v", last)
		}
	}
	expired := auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, phone(t, "0901234567"), clientIP, now)
	if sent, err := auth.SendCode(ctx, b, emailInbox{b}, codes(), expired, now.Add(otp.CodeTTL)); err != nil || sent || len(b.all()) != 2 {
		t.Fatalf("expired challenge sent: %v %v, %d messages", sent, err, len(b.all()))
	}
}

// A retried admit step carries the same write ID; a quota already holding it
// was saved by this step, so the send is not counted twice.
func TestAdmitRetryCountsOnce(t *testing.T) {
	q, dest, ctx := otptest.NewQuotas(), phone(t, "0901234567"), context.Background()
	w := uuid.NewV7()
	for range 2 {
		if err := auth.AdmitOTP(ctx, q, dest, clientIP, now, w); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"dest:" + dest.Value(), "ip:" + clientIP.String()} {
		if got, _ := q.Get(ctx, key); len(got.SentAt()) != 1 {
			t.Fatalf("%s: sends counted %d", key, len(got.SentAt()))
		}
	}
}

// IPv6 clients are limited per /64, the block one host usually holds.
func TestAdmitKeysIPv6ByPrefix(t *testing.T) {
	q, ctx := otptest.NewQuotas(), context.Background()
	for i := range 20 {
		ip := netip.MustParseAddr(fmt.Sprintf("2001:db8:1:2::%x", i+1))
		if err := auth.AdmitOTP(ctx, q, phone(t, fmt.Sprintf("09%08d", i)), ip, now, uuid.NewV7()); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	other := netip.MustParseAddr("2001:db8:1:2:ffff::1")
	if err := auth.AdmitOTP(ctx, q, phone(t, "0987654321"), other, now, uuid.NewV7()); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("21st from the same /64: %v", err)
	}
}

// An address over its limit must not spend the quota of the numbers it
// names, or one machine could lock anyone out of sign-in.
func TestAdmitChecksTheAddressFirst(t *testing.T) {
	q, ctx := otptest.NewQuotas(), context.Background()
	for i := range 20 {
		auth.AdmitOTP(ctx, q, phone(t, fmt.Sprintf("09%08d", i)), clientIP, now, uuid.NewV7())
	}
	victim := phone(t, "0987654321")
	if err := auth.AdmitOTP(ctx, q, victim, clientIP, now, uuid.NewV7()); !errors.Is(err, auth.ErrOTPLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if err := auth.AdmitOTP(ctx, q, victim, netip.MustParseAddr("198.51.100.9"), now, uuid.NewV7()); err != nil {
		t.Fatalf("the victim from their own address: %v", err)
	}
}

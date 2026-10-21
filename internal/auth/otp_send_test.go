package auth_test

import (
	"context"
	"crypto/hmac"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
)

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

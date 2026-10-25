package service_test

import (
	"context"
	"crypto/hmac"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/contact"
)

func TestNewChallengeHashesTheDerivedCode(t *testing.T) {
	t.Parallel()
	id, dest := uuid.NewV7(), phone(t, "0901234567")
	c := service.NewChallenge(codes(), id, domain.PurposeLogin, dest, clientIP, now)
	if c.ID() != id || !hmac.Equal(c.CodeHash(), codes().Hash(dest, codes().For(id))) {
		t.Fatalf("%+v", c)
	}
}

func TestSendCode(t *testing.T) {
	t.Parallel()
	ctx, b := context.Background(), &inbox{}
	email, _ := contact.ParseEmail("linh@gmail.com")
	for _, dest := range []contact.Address{phone(t, "0901234567"), email} {
		c := service.NewChallenge(codes(), uuid.NewV7(), domain.PurposeLogin, dest, clientIP, now)
		if sent, err := service.SendCode(ctx, b, emailInbox{b}, codes(), c, now); err != nil || !sent {
			t.Fatal(sent, err)
		}
		last := b.all()[len(b.all())-1]
		if last.To != dest.Value() || !strings.Contains(last.Text, codes().For(c.ID())) {
			t.Fatalf("%+v", last)
		}
	}
	expired := service.NewChallenge(codes(), uuid.NewV7(), domain.PurposeLogin, phone(t, "0901234567"), clientIP, now)
	if sent, err := service.SendCode(ctx, b, emailInbox{b}, codes(), expired, now.Add(domain.CodeTTL)); err != nil || sent || len(b.all()) != 2 {
		t.Fatalf("expired challenge sent: %v %v, %d messages", sent, err, len(b.all()))
	}
}

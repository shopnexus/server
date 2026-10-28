package domain_test

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var now = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func TestNewCredentials(t *testing.T) {
	t.Parallel()
	acc, idID := uuid.NewV7(), uuid.NewV7()
	phone, _ := contact.ParsePhone("0901234567")
	c := domain.NewCredentials(acc, domain.NewIdentity(idID, phone, now), now)
	ids := c.Identities()
	if c.AccountID() != acc || c.WriteID() != acc || len(ids) != 1 || ids[0].ID() != idID ||
		ids[0].Kind() != domain.KindPhone || ids[0].Value() != "+84901234567" || !ids[0].LastUsedAt().Equal(now) {
		t.Fatalf("%+v", c)
	}
}

func TestMarkUsed(t *testing.T) {
	t.Parallel()
	phone, _ := contact.ParsePhone("0901234567")
	id := uuid.NewV7()
	c := domain.NewCredentials(uuid.NewV7(), domain.NewIdentity(id, phone, now), now)
	if err := c.MarkUsed(id, now.Add(time.Hour)); err != nil || !c.Identities()[0].LastUsedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("%v %v", err, c.Identities()[0].LastUsedAt())
	}
	if err := c.MarkUsed(uuid.NewV7(), now); !errors.Is(err, domain.ErrUnknownIdentity) {
		t.Fatalf("unknown identity: %v", err)
	}
}

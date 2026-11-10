package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/contact"
)

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

func TestAddIdentity(t *testing.T) {
	t.Parallel()
	phone, _ := contact.ParsePhone("0901234567")
	email, _ := contact.ParseEmail("Linh@Example.com")
	other, _ := contact.ParseEmail("linh2@example.com")
	c := domain.NewCredentials(uuid.NewV7(), domain.NewIdentity(uuid.NewV7(), email, now), now)
	if c.Has(domain.KindPhone) || !c.Has(domain.KindEmail) {
		t.Fatal("Has before Add")
	}
	if err := c.Add(domain.NewIdentity(uuid.NewV7(), phone, now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if !c.Has(domain.KindPhone) || len(c.Identities()) != 2 || !c.UpdatedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("%+v", c.Identities())
	}
	if err := c.Add(domain.NewIdentity(uuid.NewV7(), other, now)); !errors.Is(err, domain.ErrIdentityKindTaken) {
		t.Fatalf("a second email: %v", err)
	}
	if len(c.Identities()) != 2 {
		t.Fatal("a refused Add changed the credentials")
	}
}

func TestCredentialsJSON(t *testing.T) {
	t.Parallel()
	email, _ := contact.ParseEmail("linh@example.com")
	c := domain.NewCredentials(uuid.NewV7(), domain.NewIdentity(uuid.NewV7(), email, now), now)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.Credentials
	if err = json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	got, want := back.Identities()[0], c.Identities()[0]
	if back.AccountID() != c.AccountID() || back.WriteID() != c.WriteID() || !back.UpdatedAt().Equal(c.UpdatedAt()) ||
		got.ID() != want.ID() || got.Kind() != domain.KindEmail || got.Value() != "linh@example.com" ||
		!got.LastUsedAt().Equal(*want.LastUsedAt()) || !got.CreatedAt().Equal(want.CreatedAt()) {
		t.Fatalf("%s", b)
	}
}

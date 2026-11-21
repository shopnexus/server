package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestFindByContactAndIdentities(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	a, err := domain.New(uuid.NewV7(), "Linh", now)
	if err != nil {
		t.Fatal(err)
	}
	id := a.ID()
	creds := repository.NewCredentials(db)
	c := domain.NewCredentials(id, domain.NewIdentity(uuid.NewV7(), phone(t, "0901234567"), now), now)
	if err = creds.SignUp(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	b, err := domain.New(uuid.NewV7(), "Hà", now)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := contact.ParseEmail("Ha@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = creds.SignUp(ctx, b, domain.NewCredentials(b.ID(), domain.NewIdentity(uuid.NewV7(), mail, now), now)); err != nil {
		t.Fatal(err)
	}
	client := service.New(service.Deps{Credentials: creds})
	for _, q := range []string{"ha@example.com", " HA@example.com "} {
		if got, ok, findErr := client.FindByContact(ctx, q); findErr != nil || !ok || got != b.ID() {
			t.Errorf("%q: %v %v %v", q, got, ok, findErr)
		}
	}
	for _, q := range []string{"0901234567", "+84901234567", "84 901 234 567"} {
		got, ok, findErr := client.FindByContact(ctx, q)
		if findErr != nil || !ok || got != id {
			t.Errorf("%q: %v %v %v", q, got, ok, findErr)
		}
	}
	for _, q := range []string{"0907654321", "nobody@example.com", "not a phone"} {
		if _, ok, findErr := client.FindByContact(ctx, q); findErr != nil || ok {
			t.Errorf("%q matched: %v %v", q, ok, findErr)
		}
	}
	ids, err := client.Identities(ctx, id)
	if err != nil || len(ids) != 1 || ids[0].Kind != domain.KindPhone || ids[0].Value != "+84901234567" {
		t.Fatalf("%+v %v", ids, err)
	}
	if none, err := client.Identities(ctx, uuid.NewV7()); err != nil || len(none) != 0 || none == nil {
		t.Fatalf("an account without credentials: %+v %v", none, err)
	}
}

func TestHasPhone(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	creds := repository.NewCredentials(db)
	svc := service.New(service.Deps{Accounts: repository.NewAccounts(db), Credentials: creds, Now: func() time.Time { return now }, Log: discard})
	signUp := func(addr contact.Address) uuid.UUID {
		a, err := domain.New(uuid.NewV7(), "", now)
		if err != nil {
			t.Fatal(err)
		}
		if err = creds.SignUp(context.Background(), a, domain.NewCredentials(a.ID(), domain.NewIdentity(uuid.NewV7(), addr, now), now)); err != nil {
			t.Fatal(err)
		}
		return a.ID()
	}
	for name, c := range map[string]struct {
		id   uuid.UUID
		want bool
	}{
		"phone":          {signUp(phone(t, "0901234567")), true},
		"email only":     {signUp(email(t, "linh@example.com")), false},
		"no credentials": {seed(t, repository.NewAccounts(db)), false},
	} {
		if got, err := svc.HasPhone(context.Background(), c.id); err != nil || got != c.want {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}

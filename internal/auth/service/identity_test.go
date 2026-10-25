package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestClientFindsAccounts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	id := newAccount(t, db)
	creds := repository.NewCredentials(db)
	c := domain.NewCredentials(id, domain.NewIdentity(uuid.NewV7(), phone(t, "0901234567"), now), now)
	if err := creds.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	client := service.New(service.Deps{Credentials: creds})
	for _, q := range []string{"0901234567", "+84901234567", "84 901 234 567"} {
		got, ok, err := client.FindAccount(ctx, q)
		if err != nil || !ok || got != id {
			t.Errorf("%q: %v %v %v", q, got, ok, err)
		}
	}
	for _, q := range []string{"0907654321", "nobody@example.com", "not a phone"} {
		if _, ok, err := client.FindAccount(ctx, q); err != nil || ok {
			t.Errorf("%q matched: %v %v", q, ok, err)
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

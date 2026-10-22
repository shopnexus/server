package account_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func newStore(t *testing.T) account.Store {
	t.Helper()
	return account.NewPGStore(pgtest.New(t))
}

func seed(t *testing.T, s account.Store) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := account.Create(context.Background(), s, id, "Linh", now); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if err := account.Create(context.Background(), s, id, "Linh", now); err != nil {
		t.Fatal(err)
	}
	if v, err := account.Get(context.Background(), s, id, now); err != nil || v.DisplayName != "Linh" || v.Status != account.StatusActive {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCreateRejectsLongName(t *testing.T) {
	t.Parallel()
	err := account.Create(context.Background(), newStore(t), uuid.NewV7(), strings.Repeat("a", 51), now)
	if !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

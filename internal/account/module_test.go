package account_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func newModule() *account.Module {
	return account.NewModule(accounttest.NewMemStore(), func() time.Time { return now })
}

func TestCreateThenStatus(t *testing.T) {
	m := newModule()
	id, err := m.Create(context.Background(), nil, "Linh")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(context.Background(), nil, id)
	if err != nil || st != account.StatusActive {
		t.Fatalf("%s %v", st, err)
	}
}

func TestStatusUnknown(t *testing.T) {
	if _, err := newModule().Status(context.Background(), nil, uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestCreateRejectsLongName(t *testing.T) {
	m := newModule()
	if _, err := m.Create(context.Background(), nil, strings.Repeat("a", 51)); !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

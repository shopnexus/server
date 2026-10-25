package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
)

func TestCreateIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if err := at(s, now).Create(context.Background(), id, "Linh"); err != nil {
		t.Fatal(err)
	}
	if v, err := at(s, now).Get(context.Background(), id); err != nil || v.DisplayName != "Linh" || v.Status != domain.StatusActive {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCreateRejectsLongName(t *testing.T) {
	t.Parallel()
	err := at(newStore(t), now).Create(context.Background(), uuid.NewV7(), strings.Repeat("a", 51))
	if !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

// Delete removes the account, and deleting it again does nothing.
func TestDelete(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	for range 2 {
		if err := at(s, now).Delete(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := at(s, now).Status(context.Background(), id); err == nil {
		t.Fatal("found after delete")
	}
}

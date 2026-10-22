package account_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

func TestClientDelegates(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	c := account.Client{Store: s, Now: func() time.Time { return now }}
	id := uuid.NewV7()
	if err := c.Create(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
	if st, err := c.Status(context.Background(), id); err != nil || st != account.StatusActive {
		t.Fatalf("%s %v", st, err)
	}
	if err := c.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background(), id); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

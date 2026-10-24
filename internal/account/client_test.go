package account_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
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
	if r, err := c.Suspend(context.Background(), id, "spam", nil, audit.ByCLI("test")); err != nil || r.Status != account.StatusSuspended {
		t.Fatalf("suspend: %+v %v", r, err)
	}
	if r, err := c.Reinstate(context.Background(), id, audit.ByCLI("test")); err != nil || r.Status != account.StatusActive {
		t.Fatalf("reinstate: %+v %v", r, err)
	}
	if r, err := c.Record(context.Background(), id); err != nil || r.ID != id {
		t.Fatalf("record: %+v %v", r, err)
	}
	if rs, err := c.Records(context.Background(), page.Request{Cursor: uuid.UUID{}, Limit: 5}); err != nil || len(rs) != 1 {
		t.Fatalf("records: %+v %v", rs, err)
	}
	if err := c.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background(), id); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

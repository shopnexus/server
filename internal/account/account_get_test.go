package account_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

func TestStatusFollowsTheClock(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	a, _ := s.Get(context.Background(), id)
	until := now.Add(time.Hour)
	if err := a.Suspend("spam", &until, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), &a, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Time
		want account.Status
	}{{now, account.StatusSuspended}, {until.Add(time.Second), account.StatusActive}} {
		if st, err := account.StatusOf(context.Background(), s, id, c.at); err != nil || st != c.want {
			t.Fatalf("at %s: %s %v", c.at, st, err)
		}
	}
}

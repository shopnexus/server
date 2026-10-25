package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
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
		want domain.Status
	}{{now, domain.StatusSuspended}, {until.Add(time.Second), domain.StatusActive}} {
		if st, err := at(s, c.at).Status(context.Background(), id); err != nil || st != c.want {
			t.Fatalf("at %s: %s %v", c.at, st, err)
		}
	}
}

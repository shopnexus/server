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

// GetMany reads the views of the accounts it finds, keyed by ID.
func TestGetMany(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	a, b := seed(t, s), seed(t, s)
	got, err := at(s, now).GetMany(context.Background(), []uuid.UUID{a, b, uuid.NewV7()})
	if err != nil || len(got) != 2 || got[a].ID != a || got[b].DisplayName != "Linh" || got[b].Status != domain.StatusActive {
		t.Fatalf("%+v %v", got, err)
	}
}

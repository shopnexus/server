package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
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
	if err := s.Save(context.Background(), &a, uuid.NewV7(), nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Time
		want domain.Status
	}{{now, domain.StatusSuspended}, {until.Add(time.Second), domain.StatusActive}} {
		if got, err := at(s, nil, c.at).Status(context.Background(), id); err != nil || got != c.want {
			t.Fatalf("at %s: %s %v", c.at, got, err)
		}
	}
}

// GetMany reads the views of the accounts it finds, keyed by ID.
func TestGetMany(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	a, b := seed(t, s), seed(t, s)
	got, err := at(s, nil, now).GetMany(context.Background(), []uuid.UUID{a, b, uuid.NewV7()})
	if err != nil || len(got) != 2 || got[a].ID != a || got[b].DisplayName != "Linh" || got[b].Status != domain.StatusActive {
		t.Fatalf("%+v %v", got, err)
	}
}

// A suspended account reads why and until when; once the end passes, or
// for an active account, there is no suspension.
// UC-ACC-06 · BR-MOD-02.
func TestMeShowsTheSuspension(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, accounts)
	me := func(when time.Time) service.MeView {
		t.Helper()
		v, err := service.New(service.Deps{
			Accounts: accounts, Credentials: repository.NewCredentials(db), Now: func() time.Time { return when }, Log: discard,
		}).Me(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := me(now); v.Suspension != nil {
		t.Fatalf("active: %+v", v.Suspension)
	}
	a, _ := accounts.Get(ctx, id)
	until := now.Add(time.Hour)
	if err := a.Suspend("Bán hàng giả", &until, now); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save(ctx, &a, uuid.NewV7(), nil); err != nil {
		t.Fatal(err)
	}
	if v := me(now); v.Status != domain.StatusSuspended || v.Suspension == nil || v.Suspension.Reason != "Bán hàng giả" ||
		v.Suspension.Until == nil || !v.Suspension.Until.Equal(until) {
		t.Fatalf("suspended: %+v", v)
	}
	if v := me(until); v.Status != domain.StatusActive || v.Suspension != nil {
		t.Fatalf("lapsed: %+v", v)
	}
}

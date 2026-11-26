package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/events"
)

// UC-ACC-06.
func TestUpdate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	v, err := at(s, nil, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new(" Minh "), Bio: new(" Đồ cũ còn tốt ")})
	if err != nil || v.DisplayName != "Minh" || v.Bio != "Đồ cũ còn tốt" {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err = at(s, nil, now).Update(context.Background(), id, service.UpdateInput{Bio: new("")}); err != nil || v.DisplayName != "Minh" || v.Bio != "" {
		t.Fatalf("bio only: %+v %v", v, err)
	}
}

// A name and a bio that break their rules are named together.
// UC-ACC-06 E1.
func TestUpdateRefusesTextRules(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	_, err := at(s, nil, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new(" "), Bio: new(strings.Repeat("a", 201))})
	if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidAccount) || len(e.Fields) != 2 {
		t.Fatalf("%v", err)
	}
}

// UC-ACC-06 E3.
func TestUpdateRefusedWhileSuspended(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	a, _ := s.Get(context.Background(), id)
	a.Suspend("spam", nil, now)
	s.Save(context.Background(), &a, uuid.NewV7(), nil)
	if _, err := at(s, nil, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new("Minh")}); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("err %v", err)
	}
}

// Away mode is stored, read back while in force, gone once it lapses, and
// cleared with ClearAway.
// UC-ACC-06 A1, E1.
func TestUpdateSetsAndClearsAway(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStore(t)
	id := seed(t, s)
	away := func(at time.Time) bool {
		t.Helper()
		v, err := service.New(service.Deps{Accounts: s, Now: func() time.Time { return at }}).Away(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	until := now.Add(72 * time.Hour)
	v, err := at(s, nil, now).Update(ctx, id, service.UpdateInput{AwayUntil: &until})
	if err != nil || v.AwayUntil == nil || !v.AwayUntil.Equal(until) || !away(now) {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err = at(s, nil, until).Get(ctx, id); err != nil || v.AwayUntil != nil || away(until) {
		t.Fatalf("at its end: %+v %v", v, err)
	}
	if v, err = at(s, nil, now).Update(ctx, id, service.UpdateInput{ClearAway: true}); err != nil || v.AwayUntil != nil || away(now) {
		t.Fatalf("cleared: %+v %v", v, err)
	}
	far := now.Add(31 * 24 * time.Hour)
	if _, err = at(s, nil, now).Update(ctx, id, service.UpdateInput{AwayUntil: &far}); !errors.Is(err, domain.ErrInvalidAccount) {
		t.Fatalf("31 days: %v", err)
	}
}

// conflicting is a store whose Save always loses a race.
type conflicting struct{ repository.Accounts }

func (conflicting) Save(context.Context, *domain.Account, uuid.UUID, *audit.Entry, ...events.Event) error {
	return repository.ErrConflict
}

// A lost race reaches the client as try_again, as in every module.
// UC-ACC-06 E4 · NFR-CONC-01.
func TestUpdateReportsAConflict(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if _, err := at(conflicting{s}, nil, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new("Minh")}); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("err %v", err)
	}
}

package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
)

func TestUpdate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	v, err := at(s, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new(" Minh "), Bio: new(" Đồ cũ còn tốt ")})
	if err != nil || v.DisplayName != "Minh" || v.Bio != "Đồ cũ còn tốt" {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err = at(s, now).Update(context.Background(), id, service.UpdateInput{Bio: new("")}); err != nil || v.DisplayName != "Minh" || v.Bio != "" {
		t.Fatalf("bio only: %+v %v", v, err)
	}
}

// A name and a bio that break their rules are named together.
func TestUpdateRefusesTextRules(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	_, err := at(s, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new(" "), Bio: new(strings.Repeat("a", 201))})
	if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidAccount) || len(e.Fields) != 2 {
		t.Fatalf("%v", err)
	}
}

func TestUpdateRefusedWhileSuspended(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	a, _ := s.Get(context.Background(), id)
	a.Suspend("spam", nil, now)
	s.Save(context.Background(), &a, uuid.NewV7())
	if _, err := at(s, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new("Minh")}); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("err %v", err)
	}
}

// conflicting is a store whose Save always loses a race.
type conflicting struct{ repository.Accounts }

func (conflicting) Save(context.Context, *domain.Account, uuid.UUID, ...events.Event) error {
	return repository.ErrConflict
}

// A lost race reaches the client as try_again, the code auth uses too.
func TestUpdateReportsAConflict(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if _, err := at(conflicting{s}, now).Update(context.Background(), id, service.UpdateInput{DisplayName: new("Minh")}); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("err %v", err)
	}
}

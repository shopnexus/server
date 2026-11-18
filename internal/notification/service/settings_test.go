package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// Quiet hours that start when they end are refused and change nothing; a
// put is read back.
func TestPutSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := service.New(service.Deps{Settings: repository.NewSettings(db), Now: time.Now})
	me := accounttest.New(t, db)
	bad := domain.DefaultSettings()
	bad.Quiet.To = bad.Quiet.From
	if err := s.PutSettings(ctx, me, bad); !errors.Is(err, domain.ErrInvalidQuietHours) {
		t.Fatalf("from == to: %v", err)
	}
	if got, _ := s.Settings(ctx, me); got != domain.DefaultSettings() {
		t.Fatalf("a refused put changed %+v", got)
	}
	want := domain.DefaultSettings()
	want.Messages, want.Quiet = false, domain.Quiet{From: "12:00", To: "13:00"}
	if err := s.PutSettings(ctx, me, want); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got, _ := s.Settings(ctx, me); got != want {
		t.Fatalf("get after put: %+v", got)
	}
}

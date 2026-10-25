package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestSuspendRecordsTheChange(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, s)
	until := now.Add(24 * time.Hour)

	r, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: id, Reason: "  spam  ", Until: &until, By: audit.ByCLI("test")})
	if err != nil || r.Status != domain.StatusSuspended || r.SuspendReason != "spam" || !r.SuspendedUntil.Equal(until) {
		t.Fatalf("%+v %v", r, err)
	}
	var action string
	var details map[string]any
	if scanErr := db.QueryRow(ctx, `SELECT action, details FROM staff_actions WHERE target_id = @id`, pgx.StrictNamedArgs{"id": id}).
		Scan(&action, &details); scanErr != nil {
		t.Fatal(scanErr)
	}
	if action != "account.suspend" || details["reason"] != "spam" || details["until"] != until.Format(time.RFC3339) {
		t.Fatalf("%s %v", action, details)
	}

	r, err = at(s, now).Reinstate(ctx, id, audit.ByCLI("test"))
	if err != nil || r.Status != domain.StatusActive || r.SuspendReason != "" || r.SuspendedUntil != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestSuspendRefusals(t *testing.T) {
	t.Parallel()
	s := repository.NewAccounts(pgtest.New(t))
	ctx := context.Background()
	id := seed(t, s)
	past := now.Add(-time.Hour)
	if _, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: id, Reason: "   ", By: audit.ByCLI("test")}); !errors.Is(err, domain.ErrSuspendReason) {
		t.Fatalf("blank reason: %v", err)
	}
	if _, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: id, Reason: "spam", Until: &past, By: audit.ByCLI("test")}); !errors.Is(err, domain.ErrSuspendUntil) {
		t.Fatalf("end in the past: %v", err)
	}
	if _, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: uuid.NewV7(), Reason: "spam", By: audit.ByCLI("test")}); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestRecordsHideALapsedSuspension(t *testing.T) {
	t.Parallel()
	s := repository.NewAccounts(pgtest.New(t))
	ctx := context.Background()
	id := seed(t, s)
	until := now.Add(time.Hour)
	if _, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: id, Reason: "spam", Until: &until, By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
	r, err := at(s, now.Add(2*time.Hour)).Record(ctx, id)
	if err != nil || r.Status != domain.StatusActive || r.SuspendReason != "" || r.SuspendedUntil != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

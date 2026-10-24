package account_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestSuspendRecordsTheChange(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := account.NewPGStore(db)
	ctx := context.Background()
	id := seed(t, s)
	until := now.Add(24 * time.Hour)

	r, err := account.Suspend(ctx, s, id, "  spam  ", &until, audit.ByCLI("test"), now)
	if err != nil || r.Status != account.StatusSuspended || r.SuspendReason != "spam" || !r.SuspendedUntil.Equal(until) {
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

	r, err = account.Reinstate(ctx, s, id, audit.ByCLI("test"), now)
	if err != nil || r.Status != account.StatusActive || r.SuspendReason != "" || r.SuspendedUntil != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestSuspendRefusals(t *testing.T) {
	t.Parallel()
	s := account.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	id := seed(t, s)
	past := now.Add(-time.Hour)
	if _, err := account.Suspend(ctx, s, id, "   ", nil, audit.ByCLI("test"), now); !errors.Is(err, account.ErrSuspendReason) {
		t.Fatalf("blank reason: %v", err)
	}
	if _, err := account.Suspend(ctx, s, id, "spam", &past, audit.ByCLI("test"), now); !errors.Is(err, account.ErrSuspendUntil) {
		t.Fatalf("end in the past: %v", err)
	}
	if _, err := account.Suspend(ctx, s, uuid.NewV7(), "spam", nil, audit.ByCLI("test"), now); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestRecordsHideALapsedSuspension(t *testing.T) {
	t.Parallel()
	s := account.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	id := seed(t, s)
	until := now.Add(time.Hour)
	if _, err := account.Suspend(ctx, s, id, "spam", &until, audit.ByCLI("test"), now); err != nil {
		t.Fatal(err)
	}
	r, err := account.GetRecord(ctx, s, id, now.Add(2*time.Hour))
	if err != nil || r.Status != account.StatusActive || r.SuspendReason != "" || r.SuspendedUntil != nil {
		t.Fatalf("%+v %v", r, err)
	}
}

package staff_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/session"
)

func TestRefreshRotates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, _, in := e.enrolled(t, "mod@example.com")
	out, err := staff.Refresh(ctx, e.stores, e.keys, in.RefreshToken(), clientIP, now.Add(time.Minute))
	if err != nil || out.RefreshToken() == in.RefreshToken() || out.Access.TokenType != "Bearer" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := staff.Refresh(ctx, e.stores, e.keys, out.RefreshToken(), clientIP, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("the rotated token: %v", err)
	}
	if _, err := staff.Refresh(ctx, e.stores, e.keys, "rt_unknown", clientIP, now); !errors.Is(err, session.ErrInvalidRefresh) {
		t.Fatalf("an unknown token: %v", err)
	}
	if _, err := staff.Refresh(ctx, e.stores, e.keys, out.RefreshToken(), clientIP, now.Add(session.TTL)); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("a rotated token used again: %v", err)
	}
}

func TestRefreshReuseEndsTheSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, _, in := e.enrolled(t, "mod@example.com")
	out, err := staff.Refresh(ctx, e.stores, e.keys, in.RefreshToken(), clientIP, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = staff.Refresh(ctx, e.stores, e.keys, in.RefreshToken(), clientIP, now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("replay: %v", err)
	}
	if _, err = staff.Refresh(ctx, e.stores, e.keys, out.RefreshToken(), clientIP, now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("the newer token after a replay: %v", err)
	}
	var n int
	err = e.db.QueryRow(ctx, `SELECT count(*) FROM staff_actions WHERE action = 'staff.refresh_reuse' AND target_id = @id`,
		pgx.StrictNamedArgs{"id": in.SignIn.Staff.ID}).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("audit rows %d: %v", n, err)
	}
}

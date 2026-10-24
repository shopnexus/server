package staff_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

func TestChangePasswordKeepsOnlyThisSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	password, secret, first := e.enrolled(t, "mod@example.com")
	later := now.Add(time.Minute)
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "mod@example.com", password, clientIP, later)
	if err != nil {
		t.Fatal(err)
	}
	second, err := staff.VerifyTOTP(ctx, e.stores, e.keys, step.Ticket, member.Code(secret, later), clientIP, "test", later)
	if err != nil {
		t.Fatal(err)
	}
	if err := staff.ChangePassword(ctx, e.stores, e.keys, e.callerOf(t, second), "wrong current", "another long password", later); !errors.Is(err, member.ErrInvalidCredentials) {
		t.Fatalf("a wrong current password: %v", err)
	}
	if err := staff.ChangePassword(ctx, e.stores, e.keys, e.callerOf(t, second), password, password, later); !errors.Is(err, member.ErrPasswordTooWeak) {
		t.Fatalf("the same password: %v", err)
	}
	if err := staff.ChangePassword(ctx, e.stores, e.keys, e.callerOf(t, second), password, "another long password", later); err != nil {
		t.Fatal(err)
	}
	if w := get(t, guarded(e, member.RoleModerator, later), second.SignIn.AccessToken); w.Code != 200 {
		t.Fatalf("this session: %d", w.Code)
	}
	if w := get(t, guarded(e, member.RoleModerator, later), first.SignIn.AccessToken); w.Code != 401 {
		t.Fatalf("the other session: %d", w.Code)
	}
	if _, err := staff.Refresh(ctx, e.stores, e.keys, first.RefreshToken(), clientIP, later); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("the other session's refresh: %v", err)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, _, in := e.enrolled(t, "mod@example.com")
	if err := staff.Logout(ctx, e.stores.Sessions, e.callerOf(t, in), now); err != nil {
		t.Fatal(err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.SignIn.AccessToken); w.Code != 401 {
		t.Fatalf("after logout: %d", w.Code)
	}
	me, err := staff.GetMe(ctx, e.stores.Members, e.callerOf(t, in))
	if err != nil || me.Email != "mod@example.com" {
		t.Fatalf("%+v %v", me, err)
	}
}

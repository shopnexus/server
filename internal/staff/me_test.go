package staff_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

func TestChangePasswordEndsEverySession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret, first := e.enrolled(t, "mod@example.com")
	later := now.Add(time.Minute)
	second, err := e.login("mod@example.com", password, "", member.Code(secret, later), later)
	if err != nil {
		t.Fatal(err)
	}
	caller := e.callerOf(t, second)
	if err := staff.ChangePassword(ctx, e.stores.Members, e.keys, caller, "wrong current", "another long password", later); !errors.Is(err, member.ErrInvalidCredentials) {
		t.Fatalf("a wrong current password: %v", err)
	}
	if err := staff.ChangePassword(ctx, e.stores.Members, e.keys, caller, password, password, later); !errors.Is(err, member.ErrPasswordTooWeak) {
		t.Fatalf("the same password: %v", err)
	}
	if err := staff.ChangePassword(ctx, e.stores.Members, e.keys, caller, password, "another long password", later); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]staff.SignedIn{"this session": second, "the other session": first} {
		if w := get(t, guarded(e, member.RoleModerator, later), in.Token); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

// A disable that lands after staff.Require let the request in still stops
// the change.
func TestChangePasswordRefusesADisabledMember(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com")
	caller := e.callerOf(t, in)
	if _, err := staff.DisableMember(ctx, e.stores.Members, caller.StaffID, audit.ByCLI("root"), now); err != nil {
		t.Fatal(err)
	}
	if err := staff.ChangePassword(ctx, e.stores.Members, e.keys, caller, password, "another long password", now); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("change password: %v", err)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com")
	caller := e.callerOf(t, in)
	if err := staff.Logout(ctx, e.stores.Sessions, caller, now); err != nil {
		t.Fatal(err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.Token); w.Code != 401 {
		t.Fatalf("after logout: %d", w.Code)
	}
	if err := staff.Logout(ctx, e.stores.Sessions, caller, now); err != nil {
		t.Fatalf("a second logout: %v", err)
	}
	me, err := staff.GetMe(ctx, e.stores.Members, caller)
	if err != nil || me.Email != "mod@example.com" {
		t.Fatalf("%+v %v", me, err)
	}
}

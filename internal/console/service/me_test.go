package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/console/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
)

func TestChangePasswordEndsEverySession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	secret, first := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	later := now.Add(time.Minute)
	second, err := e.login("mod@example.com", password, "", domain.Code(secret, later), later)
	if err != nil {
		t.Fatal(err)
	}
	caller := e.callerOf(t, second)
	if err := e.at(later).ChangePassword(ctx, caller, "wrong current", "another long password"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("a wrong current password: %v", err)
	}
	if err := e.at(later).ChangePassword(ctx, caller, password, password); !errors.Is(err, domain.ErrPasswordTooWeak) {
		t.Fatalf("the same password: %v", err)
	}
	if err := e.at(later).ChangePassword(ctx, caller, password, "another long password"); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]service.SignedIn{"this session": second, "the other session": first} {
		if _, err := e.at(later).Authenticate(context.Background(), in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A disable that lands after Require let the request in still stops
// the change.
func TestChangePasswordRefusesADisabledMember(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	caller := e.callerOf(t, in)
	if _, err := e.at(now).DisableMember(ctx, caller.MemberID, audit.ByCLI("root")); err != nil {
		t.Fatal(err)
	}
	if err := e.at(now).ChangePassword(ctx, caller, password, "another long password"); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("change password: %v", err)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	caller := e.callerOf(t, in)
	if err := e.at(now).Logout(ctx, caller); err != nil {
		t.Fatal(err)
	}
	if _, err := e.at(now).Authenticate(context.Background(), in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("after logout: %v", err)
	}
	if err := e.at(now).Logout(ctx, caller); err != nil {
		t.Fatalf("a second logout: %v", err)
	}
	me, err := e.at(now).GetMe(ctx, caller)
	if err != nil || me.Email != "mod@example.com" {
		t.Fatalf("%+v %v", me, err)
	}
}

// The agent's MCP server reads an active member only.
func TestActiveMemberRefusesUnknownAndDisabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, _ := e.create(t, "agent-mod@example.com", domain.RoleModerator)
	if got, err := e.at(now).ActiveMember(ctx, v.ID); err != nil || got.Role != domain.RoleModerator {
		t.Fatalf("an active member: %+v %v", got, err)
	}
	if _, err := e.at(now).ActiveMember(ctx, uuid.NewV7()); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("an unknown member: %v", err)
	}
	if _, err := e.at(now).DisableMember(ctx, v.ID, audit.ByCLI("root")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.at(now).ActiveMember(ctx, v.ID); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("a disabled member: %v", err)
	}
}

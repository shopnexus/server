package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff/domain"
)

func TestAdminCannotTouchAnAdmin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	admin, _ := e.create(t, "admin@example.com", domain.RoleAdmin)
	adminActor := audit.Actor{Kind: audit.ActorStaff, StaffID: uuid.NewV7(), IP: clientIP}
	if _, err := e.at(now).DisableMember(ctx, admin.ID, adminActor); !errors.Is(err, domain.ErrAdminManagedByCLI) {
		t.Fatalf("disable: %v", err)
	}
	if _, err := e.at(now).ResetMemberPassword(ctx, admin.ID, adminActor); !errors.Is(err, domain.ErrAdminManagedByCLI) {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := e.at(now).DisableMember(ctx, admin.ID, audit.ByCLI("root")); err != nil {
		t.Fatalf("the CLI: %v", err)
	}
	if _, err := e.at(now).EnableMember(ctx, uuid.NewV7(), audit.ByCLI("root")); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("an unknown id: %v", err)
	}
}

func TestResetsEndSessionsAndStartOver(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com")
	temp, err := e.at(now).ResetMemberPassword(ctx, in.Me.ID, audit.ByCLI("root"))
	if err != nil || len(temp) != 16 {
		t.Fatalf("%q %v", temp, err)
	}
	if _, err = e.at(now).Authenticate(context.Background(), in.Token); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("a session after the reset: %v", err)
	}
	if _, err = e.login("mod@example.com", temp, "", "", now); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatalf("the temporary password: %v", err)
	}
	v, err := e.at(now).ResetMemberTOTP(ctx, in.Me.ID, audit.ByCLI("root"))
	if err != nil || v.TOTPEnrolled {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestUnlockAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, temp := e.create(t, "mod@example.com", domain.RoleModerator)
	for range domain.MaxFailures {
		_, _ = e.login("mod@example.com", "wrong password", "", "", now)
	}
	list, err := e.at(now).ListMembers(ctx, page.Request{Limit: 10})
	if err != nil || len(list.Items) != 1 || list.Items[0].LockedUntil == nil || !list.Items[0].LockedUntil.Equal(now.Add(domain.LockFor)) {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err = e.at(now).UnlockMember(ctx, v.ID, audit.ByCLI("root")); err != nil {
		t.Fatal(err)
	}
	if _, err = e.login("mod@example.com", temp, "", "", now.Add(time.Second)); !errors.Is(err, domain.ErrPasswordChangeRequired) {
		t.Fatalf("after unlock: %v", err)
	}
	acts, err := audit.NewPGStore(e.db).List(ctx, audit.Filter{TargetID: v.ID}, page.Request{Limit: 10})
	if err != nil || len(acts) != 2 || acts[0].Action != audit.StaffUnlock || acts[1].Action != audit.StaffCreate || acts[1].Details["role"] != "moderator" {
		t.Fatalf("%+v %v", acts, err)
	}
}

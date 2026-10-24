package staff_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

func TestAdminCannotTouchAnAdmin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	admin, _ := e.create(t, "admin@example.com", member.RoleAdmin)
	adminActor := audit.Actor{Kind: audit.ActorStaff, StaffID: uuid.NewV7(), IP: clientIP}
	if _, err := staff.DisableMember(ctx, e.stores.Members, admin.ID, adminActor, now); !errors.Is(err, member.ErrAdminManagedByCLI) {
		t.Fatalf("disable: %v", err)
	}
	if _, err := staff.ResetMemberPassword(ctx, e.stores.Members, e.keys.Hasher, admin.ID, adminActor, now); !errors.Is(err, member.ErrAdminManagedByCLI) {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := staff.DisableMember(ctx, e.stores.Members, admin.ID, audit.ByCLI("root"), now); err != nil {
		t.Fatalf("the CLI: %v", err)
	}
	if _, err := staff.EnableMember(ctx, e.stores.Members, uuid.NewV7(), audit.ByCLI("root"), now); !errors.Is(err, staff.ErrMemberNotFound) {
		t.Fatalf("an unknown id: %v", err)
	}
}

func TestResetsEndSessionsAndStartOver(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com")
	temp, err := staff.ResetMemberPassword(ctx, e.stores.Members, e.keys.Hasher, in.Me.ID, audit.ByCLI("root"), now)
	if err != nil || len(temp) != 16 {
		t.Fatalf("%q %v", temp, err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.Token); w.Code != 401 {
		t.Fatalf("a session after the reset: %d", w.Code)
	}
	if _, err = e.login("mod@example.com", temp, "", "", now); !errors.Is(err, staff.ErrPasswordChangeRequired) {
		t.Fatalf("the temporary password: %v", err)
	}
	v, err := staff.ResetMemberTOTP(ctx, e.stores.Members, in.Me.ID, audit.ByCLI("root"), now)
	if err != nil || v.TOTPEnrolled {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestUnlockAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, temp := e.create(t, "mod@example.com", member.RoleModerator)
	for range staff.MaxFailures {
		_, _ = e.login("mod@example.com", "wrong password", "", "", now)
	}
	list, err := staff.ListMembers(ctx, e.stores.Members, e.stores.Locks, e.keys, page.Request{Limit: 10}, now)
	if err != nil || len(list.Items) != 1 || list.Items[0].LockedUntil == nil || !list.Items[0].LockedUntil.Equal(now.Add(staff.LockFor)) {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err = staff.UnlockMember(ctx, e.stores.Members, e.stores.Locks, e.keys, v.ID, audit.ByCLI("root"), now); err != nil {
		t.Fatal(err)
	}
	if _, err = e.login("mod@example.com", temp, "", "", now.Add(time.Second)); !errors.Is(err, staff.ErrPasswordChangeRequired) {
		t.Fatalf("after unlock: %v", err)
	}
	acts, err := e.stores.Actions.List(ctx, audit.Filter{TargetID: v.ID}, page.Request{Limit: 10})
	if err != nil || len(acts) != 2 || acts[0].Action != "staff.unlock" || acts[1].Action != "staff.create" || acts[1].Details["role"] != "moderator" {
		t.Fatalf("%+v %v", acts, err)
	}
}

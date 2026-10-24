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
	"github.com/shopnexus/server2/internal/staff/quota"
)

func TestAdminCannotTouchAnAdmin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	admin, _ := e.create(t, "admin@example.com", member.RoleAdmin)
	by := audit.Actor{Kind: audit.ActorStaff, StaffID: uuid.NewV7(), SessionID: uuid.UUID{}, Name: "", IP: clientIP}
	if _, err := staff.DisableMember(ctx, e.stores, admin.ID, by, now); !errors.Is(err, member.ErrAdminManagedByCLI) {
		t.Fatalf("disable: %v", err)
	}
	if _, err := staff.ResetMemberPassword(ctx, e.stores, e.keys, admin.ID, by, now); !errors.Is(err, member.ErrAdminManagedByCLI) {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := staff.DisableMember(ctx, e.stores, admin.ID, audit.ByCLI("root"), now); err != nil {
		t.Fatalf("the CLI: %v", err)
	}
	if _, err := staff.EnableMember(ctx, e.stores, uuid.NewV7(), audit.ByCLI("root"), now); !errors.Is(err, staff.ErrMemberNotFound) {
		t.Fatalf("an unknown id: %v", err)
	}
}

func TestResetsEndSessionsAndStartOver(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, _, in := e.enrolled(t, "mod@example.com")
	id := in.SignIn.Staff.ID
	temp, err := staff.ResetMemberPassword(ctx, e.stores, e.keys, id, audit.ByCLI("root"), now)
	if err != nil || len(temp) != 16 {
		t.Fatalf("%q %v", temp, err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.SignIn.AccessToken); w.Code != 401 {
		t.Fatalf("a session after the reset: %d", w.Code)
	}
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "mod@example.com", temp, clientIP, now)
	if err != nil || step.Next != member.StepChangePassword {
		t.Fatalf("%+v %v", step, err)
	}
	v, err := staff.ResetMemberTOTP(ctx, e.stores, id, audit.ByCLI("root"), now)
	if err != nil || v.TOTPEnrolled {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestUnlockAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, temp := e.create(t, "mod@example.com", member.RoleModerator)
	for range quota.MaxFailures {
		_, _ = staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "mod@example.com", "wrong password", clientIP, now)
	}
	list, err := staff.ListMembers(ctx, e.stores, e.keys, page.Request{Limit: 10}, now)
	if err != nil || len(list.Items) != 1 || list.Items[0].LockedUntil == nil || !list.Items[0].LockedUntil.Equal(now.Add(quota.LockFor)) {
		t.Fatalf("%+v %v", list, err)
	}
	if _, err := staff.UnlockMember(ctx, e.stores, e.keys, v.ID, audit.ByCLI("root"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, "mod@example.com", temp, clientIP, now.Add(time.Second)); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
}

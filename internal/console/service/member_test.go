package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
)

func TestAdminCannotTouchAnAdmin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	admin, _ := e.create(t, "admin@example.com", domain.RoleAdmin)
	adminActor := audit.Actor{Kind: audit.ActorMember, MemberID: uuid.NewV7(), IP: clientIP}
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

// A repeated disable or enable writes no second audit row.
func TestDisableAndEnableOnlyOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, _ := e.create(t, "mod@example.com", domain.RoleModerator)
	for range 2 {
		if _, err := e.at(now).DisableMember(ctx, v.ID, audit.ByCLI("root")); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := e.at(now).EnableMember(ctx, v.ID, audit.ByCLI("root")); err != nil {
			t.Fatal(err)
		}
	}
	var disables, enables int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action_kind = 'member.disable'), count(*) FILTER (WHERE action_kind = 'member.enable')
FROM console_actions WHERE target_id = @id`, pgx.StrictNamedArgs{"id": v.ID}).Scan(&disables, &enables); err != nil {
		t.Fatal(err)
	}
	if disables != 1 || enables != 1 {
		t.Fatalf("disables %d, enables %d", disables, enables)
	}
}

func TestResetsEndSessionsAndStartOver(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
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
	if err != nil || len(acts) != 2 || acts[0].Action != audit.MemberUnlock || acts[1].Action != audit.MemberCreate || acts[1].Details["role"] != "moderator" {
		t.Fatalf("%+v %v", acts, err)
	}
}

func TestRenameAndLastSignIn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	fresh, _ := e.create(t, "new@example.com", domain.RoleModerator)
	_, in := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	admin, _ := e.create(t, "admin@example.com", domain.RoleAdmin)
	by := audit.Actor{Kind: audit.ActorMember, MemberID: admin.ID, IP: clientIP}

	v, err := e.at(now).RenameMember(ctx, in.Me.ID, "  Lan Nguyễn ", by)
	if err != nil || v.DisplayName != "Lan Nguyễn" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.at(now).RenameMember(ctx, in.Me.ID, " ", by); !errors.Is(err, domain.ErrInvalidMember) {
		t.Fatalf("a blank name: %v", err)
	}
	if _, err = e.at(now).RenameMember(ctx, admin.ID, "Boss", by); !errors.Is(err, domain.ErrAdminManagedByCLI) {
		t.Fatalf("an admin: %v", err)
	}
	acts, err := audit.NewPGStore(e.db).List(ctx, audit.Filter{TargetID: in.Me.ID}, page.Request{Limit: 1})
	if err != nil || len(acts) == 0 || acts[0].Action != audit.MemberRename || acts[0].Details["to"] != "Lan Nguyễn" || acts[0].Target.Name != "Lan Nguyễn" {
		t.Fatalf("%+v %v", acts, err)
	}

	list, err := e.at(now).ListMembers(ctx, page.Request{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list.Items {
		switch m.ID {
		case in.Me.ID:
			if m.LastSignInAt == nil || m.MustChangePassword {
				t.Errorf("signed in: %+v", m)
			}
		case fresh.ID:
			if m.LastSignInAt != nil || !m.MustChangePassword {
				t.Errorf("never signed in: %+v", m)
			}
		default:
		}
	}
}

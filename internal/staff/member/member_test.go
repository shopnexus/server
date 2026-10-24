package member_test

import (
	"errors"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/member"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

func newMember(t *testing.T, role member.Role) member.Member {
	t.Helper()
	m, err := member.New(uuid.NewV7(), " Mod@Example.COM ", " Mod One ", role, member.Fast.Hash("temporary-pass"), now)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNewNormalises(t *testing.T) {
	t.Parallel()
	m := newMember(t, member.RoleModerator)
	if m.Email() != "mod@example.com" || m.DisplayName() != "Mod One" || m.Status() != member.StatusActive ||
		m.CredentialsVersion() != 1 || m.Next() != member.StepChangePassword || m.WriteID() != m.ID() {
		t.Fatalf("%+v", m)
	}
}

func TestNewRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		email, name string
		role        member.Role
	}{
		{"not-an-email", "Mod", member.RoleModerator},
		{"mod@example.com", "  ", member.RoleModerator},
		{"mod@example.com", string(make([]rune, 101)), member.RoleModerator},
		{"mod@example.com", "Mod", member.Role("owner")},
	}
	for _, c := range cases {
		if _, err := member.New(uuid.NewV7(), c.email, c.name, c.role, "x", now); !errors.Is(err, member.ErrInvalidMember) {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestNextFollowsTheMember(t *testing.T) {
	t.Parallel()
	m := newMember(t, member.RoleModerator)
	m.SetPassword(member.Fast.Hash("a-long-new-password"), now)
	if m.Next() != member.StepEnrollTOTP || m.CredentialsVersion() != 2 {
		t.Fatalf("after the password: %s v%d", m.Next(), m.CredentialsVersion())
	}
	secret := member.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	if m.Next() != member.StepEnrollTOTP || !m.Enrolling() || m.Enrolled() {
		t.Fatalf("enrolling: %s", m.Next())
	}
	if err := m.AcceptCode(member.Code(secret, now), now); err != nil {
		t.Fatal(err)
	}
	if m.Next() != member.StepTOTP || !m.Enrolled() || m.Enrolling() {
		t.Fatalf("enrolled: %s", m.Next())
	}
}

func TestAcceptCode(t *testing.T) {
	t.Parallel()
	m := newMember(t, member.RoleModerator)
	secret := member.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	cur := member.Code(secret, now)
	wrong := cur[:5] + string(rune('0'+(cur[5]-'0'+1)%10))
	if err := m.AcceptCode(wrong, now); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := m.AcceptCode(member.Code(secret, now.Add(-30*time.Second)), now); err != nil {
		t.Fatalf("the previous step: %v", err)
	}
	if err := m.AcceptCode(member.Code(secret, now.Add(-30*time.Second)), now); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("the same code again: %v", err)
	}
	if err := m.AcceptCode(member.Code(secret, now), now); err != nil {
		t.Fatalf("the current step after the previous one: %v", err)
	}
	if err := m.AcceptCode(member.Code(secret, now.Add(-30*time.Second)), now.Add(30*time.Second)); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("a step already passed: %v", err)
	}
	if err := m.AcceptCode(member.Code(secret, now.Add(-time.Minute)), now.Add(2*time.Minute)); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("three steps old: %v", err)
	}
	fresh := newMember(t, member.RoleModerator)
	if err := fresh.AcceptCode("123456", now); !errors.Is(err, member.ErrInvalidTOTPCode) {
		t.Fatalf("no secret at all: %v", err)
	}
}

func TestVersionMovesWithCredentials(t *testing.T) {
	t.Parallel()
	m := newMember(t, member.RoleModerator)
	v := m.CredentialsVersion()
	m.Disable(now)
	m.Enable(now)
	m.ResetTOTP(now)
	m.ResetPassword(member.Fast.Hash("temporary-two"), now)
	if m.CredentialsVersion() != v+3 || m.Status() != member.StatusActive || m.Next() != member.StepChangePassword || m.Enrolled() {
		t.Fatalf("v%d %s %s", m.CredentialsVersion(), m.Status(), m.Next())
	}
}

func TestRoles(t *testing.T) {
	t.Parallel()
	if !member.RoleAdmin.AtLeast(member.RoleModerator) || member.RoleModerator.AtLeast(member.RoleAdmin) ||
		!member.RoleModerator.AtLeast(member.RoleModerator) || member.Role("owner").AtLeast(member.RoleModerator) {
		t.Fatal("role order")
	}
}

func TestManagedBy(t *testing.T) {
	t.Parallel()
	admin, mod := newMember(t, member.RoleAdmin), newMember(t, member.RoleModerator)
	staff := audit.Actor{Kind: audit.ActorStaff, StaffID: uuid.NewV7(), SessionID: uuid.UUID{}, Name: "", IP: clientIP}
	if err := admin.ManagedBy(staff); !errors.Is(err, member.ErrAdminManagedByCLI) {
		t.Fatalf("an admin by staff: %v", err)
	}
	if admin.ManagedBy(audit.ByCLI("root")) != nil || mod.ManagedBy(staff) != nil {
		t.Fatal("the CLI manages anyone; staff manage moderators")
	}
}

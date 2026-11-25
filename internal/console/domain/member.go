// Package domain holds module console's entities: the member with its role,
// password and TOTP secret, its sessions and the sign-in lock. Hashing,
// codes and the password policy live here; the repository seals TOTP secrets
// at rest.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/contact"
)

type Role string

const (
	RoleModerator Role = "moderator"
	RoleAdmin     Role = "admin"
)

// AtLeast reports whether r grants what floor does; an admin is also a
// moderator. An unknown role grants nothing.
func (r Role) AtLeast(floor Role) bool { return rank(r) > 0 && rank(r) >= rank(floor) }

func rank(r Role) int {
	switch r {
	case RoleAdmin:
		return 2
	case RoleModerator:
		return 1
	default:
		return 0
	}
}

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Step is the sign-in call a member makes next.
type Step string

const (
	StepChangePassword Step = "change_password"
	StepEnrollTOTP     Step = "enroll_totp"
	StepTOTP           Step = "totp"
)

const maxNameRunes = 100

// Member is one moderator or admin.
type Member struct{ s MemberState }

// NewMember builds an active member who replaces the password at the first
// sign-in. The first write ID is the id.
func NewMember(id uuid.UUID, email, displayName string, role Role, passwordHash string, now time.Time) (Member, error) {
	addr, err := contact.ParseEmail(email)
	name, ok := cleanName(displayName)
	if err != nil || !ok || rank(role) == 0 {
		return Member{}, ErrInvalidMember
	}
	return Member{s: MemberState{
		ID: id, Email: addr.Value(), DisplayName: name, Role: role, Status: StatusActive, PasswordHash: passwordHash,
		MustChangePassword: true, CredentialsVersion: 1, CreatedAt: now, UpdatedAt: now, WriteID: id,
	}}, nil
}

func (m *Member) ID() uuid.UUID            { return m.s.ID }
func (m *Member) Email() string            { return m.s.Email }
func (m *Member) DisplayName() string      { return m.s.DisplayName }
func (m *Member) Role() Role               { return m.s.Role }
func (m *Member) Status() Status           { return m.s.Status }
func (m *Member) PasswordHash() string     { return m.s.PasswordHash }
func (m *Member) CredentialsVersion() int  { return m.s.CredentialsVersion }
func (m *Member) Enrolled() bool           { return m.s.TOTPSecret != nil }
func (m *Member) MustChangePassword() bool { return m.s.MustChangePassword }
func (m *Member) Enrolling() bool          { return m.s.TOTPPending != nil }
func (m *Member) CreatedAt() time.Time     { return m.s.CreatedAt }
func (m *Member) WriteID() uuid.UUID       { return m.s.WriteID }

// MemberState is every stored field of a member, with the TOTP secrets
// opened. Stores only.
type MemberState struct {
	ID                 uuid.UUID
	Email              string
	DisplayName        string
	Role               Role
	Status             Status
	PasswordHash       string
	MustChangePassword bool
	CredentialsVersion int
	TOTPSecret         []byte
	TOTPPending        []byte
	TOTPLastStep       int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	WriteID            uuid.UUID
}

// RestoreMember rebuilds a stored member. Stores only.
func RestoreMember(st MemberState) Member { return Member{s: st} }

// State returns every stored field of m. Stores only.
func (m *Member) State() MemberState { return m.s }

// Saved records that a store wrote m under writeID. Stores only.
func (m *Member) Saved(writeID uuid.UUID) { m.s.WriteID = writeID }

// Next is computed from the member, never taken from the request, so a client
// cannot skip a step.
func (m *Member) Next() Step {
	switch {
	case m.s.MustChangePassword:
		return StepChangePassword
	case m.s.TOTPSecret == nil:
		return StepEnrollTOTP
	default:
		return StepTOTP
	}
}

// CheckNewPassword refuses a password that breaks CheckPolicy or is the
// current one.
func (m *Member) CheckNewPassword(password string) error {
	if err := CheckPolicy(password, m.s.Email); err != nil {
		return err
	}
	if VerifyPassword(m.s.PasswordHash, password) {
		return ErrPasswordTooWeak
	}
	return nil
}

// SetPassword stores a password the member chose.
func (m *Member) SetPassword(hash string, now time.Time) {
	m.s.PasswordHash, m.s.MustChangePassword = hash, false
	m.s.CredentialsVersion++
	m.s.UpdatedAt = now
}

// ResetPassword stores a temporary password the member must replace.
func (m *Member) ResetPassword(hash string, now time.Time) {
	m.s.PasswordHash, m.s.MustChangePassword = hash, true
	m.s.CredentialsVersion++
	m.s.UpdatedAt = now
}

// StartEnrollment keeps secret until the first correct code confirms it; a
// second call replaces it.
func (m *Member) StartEnrollment(secret []byte, now time.Time) {
	m.s.TOTPPending = secret
	m.s.UpdatedAt = now
}

// AcceptCode checks code against the enrolled secret, or against the pending
// one, which a correct code then enrolls. A code whose time step is not past
// the last accepted one is refused.
func (m *Member) AcceptCode(code string, now time.Time) error {
	secret, enrolling := m.s.TOTPSecret, false
	if secret == nil {
		secret, enrolling = m.s.TOTPPending, true
	}
	if secret == nil {
		return ErrInvalidTOTPCode
	}
	step, ok := matchCode(secret, code, now, m.s.TOTPLastStep)
	if !ok {
		return ErrInvalidTOTPCode
	}
	if enrolling {
		m.s.TOTPSecret, m.s.TOTPPending = secret, nil
	}
	m.s.TOTPLastStep = step
	m.s.UpdatedAt = now
	return nil
}

// ResetTOTP removes the secret; the member enrolls again at the next
// sign-in, and every session ends.
func (m *Member) ResetTOTP(now time.Time) {
	m.s.TOTPSecret, m.s.TOTPPending = nil, nil
	m.s.CredentialsVersion++
	m.s.UpdatedAt = now
}

// Disable stops the member signing in and ends every session; Enable does
// not bring the sessions back. Each reports false when the member already
// is in that status, and changes nothing then.
func (m *Member) Disable(now time.Time) bool {
	if m.s.Status == StatusDisabled {
		return false
	}
	m.s.Status = StatusDisabled
	m.s.CredentialsVersion++
	m.s.UpdatedAt = now
	return true
}

func (m *Member) Enable(now time.Time) bool {
	if m.s.Status == StatusActive {
		return false
	}
	m.s.Status = StatusActive
	m.s.UpdatedAt = now
	return true
}

// cleanName trims a display name and reports whether it has 1 to
// maxNameRunes runes.
func cleanName(s string) (string, bool) {
	name := strings.TrimSpace(s)
	n := utf8.RuneCountInString(name)
	return name, n > 0 && n <= maxNameRunes
}

// Rename sets the display name under NewMember's rules.
func (m *Member) Rename(displayName string, now time.Time) error {
	name, ok := cleanName(displayName)
	if !ok {
		return ErrInvalidMember
	}
	m.s.DisplayName = name
	m.s.UpdatedAt = now
	return nil
}

// ManagedBy refuses to let anyone but the CLI change an admin.
func (m *Member) ManagedBy(a audit.Actor) error {
	if m.s.Role == RoleAdmin && a.Kind != audit.ActorCLI {
		return ErrAdminManagedByCLI
	}
	return nil
}

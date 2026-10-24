// Package member holds the staff member aggregate: email, role, status, the
// password hash and the TOTP secret. Hashing, codes and the password policy
// live here; the store seals TOTP secrets at rest.
package member

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/contact"
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

// Member is one moderator or admin. The store in this package reads the
// fields directly.
type Member struct {
	id                 uuid.UUID
	email              string
	displayName        string
	role               Role
	status             Status
	passwordHash       string
	mustChangePassword bool
	credentialsVersion int
	totpSecret         []byte
	totpPending        []byte
	totpLastStep       int64
	createdAt          time.Time
	updatedAt          time.Time
	writeID            uuid.UUID
}

// New builds an active member who replaces the password at the first
// sign-in. The first write ID is the id.
func New(id uuid.UUID, email, displayName string, role Role, passwordHash string, now time.Time) (Member, error) {
	addr, err := contact.ParseEmail(email)
	name := strings.TrimSpace(displayName)
	n := utf8.RuneCountInString(name)
	if err != nil || n == 0 || n > maxNameRunes || rank(role) == 0 {
		return Member{}, ErrInvalidMember
	}
	return Member{
		id: id, email: addr.Value(), displayName: name, role: role, status: StatusActive, passwordHash: passwordHash,
		mustChangePassword: true, credentialsVersion: 1, totpSecret: nil, totpPending: nil, totpLastStep: 0,
		createdAt: now, updatedAt: now, writeID: id,
	}, nil
}

// NormalizeEmail is the form emails are stored and looked up in.
func NormalizeEmail(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

func (m *Member) ID() uuid.UUID           { return m.id }
func (m *Member) Email() string           { return m.email }
func (m *Member) DisplayName() string     { return m.displayName }
func (m *Member) Role() Role              { return m.role }
func (m *Member) Status() Status          { return m.status }
func (m *Member) PasswordHash() string    { return m.passwordHash }
func (m *Member) CredentialsVersion() int { return m.credentialsVersion }
func (m *Member) Enrolled() bool          { return m.totpSecret != nil }
func (m *Member) Enrolling() bool         { return m.totpPending != nil }
func (m *Member) CreatedAt() time.Time    { return m.createdAt }
func (m *Member) WriteID() uuid.UUID      { return m.writeID }

// Next is computed from the member, never taken from a ticket, so a client
// cannot skip a step.
func (m *Member) Next() Step {
	switch {
	case m.mustChangePassword:
		return StepChangePassword
	case m.totpSecret == nil:
		return StepEnrollTOTP
	default:
		return StepTOTP
	}
}

// SetPassword stores a password the member chose.
func (m *Member) SetPassword(hash string, now time.Time) {
	m.passwordHash, m.mustChangePassword = hash, false
	m.credentialsVersion++
	m.updatedAt = now
}

// ResetPassword stores a temporary password the member must replace.
func (m *Member) ResetPassword(hash string, now time.Time) {
	m.passwordHash, m.mustChangePassword = hash, true
	m.credentialsVersion++
	m.updatedAt = now
}

// StartEnrollment keeps secret until the first correct code confirms it; a
// second call replaces it.
func (m *Member) StartEnrollment(secret []byte, now time.Time) {
	m.totpPending = secret
	m.updatedAt = now
}

// AcceptCode checks code against the enrolled secret, or against the pending
// one, which a correct code then enrolls. A code whose time step is not past
// the last accepted one is refused.
func (m *Member) AcceptCode(code string, now time.Time) error {
	secret, enrolling := m.totpSecret, false
	if secret == nil {
		secret, enrolling = m.totpPending, true
	}
	if secret == nil {
		return ErrInvalidTOTPCode
	}
	step, ok := matchCode(secret, code, now, m.totpLastStep)
	if !ok {
		return ErrInvalidTOTPCode
	}
	if enrolling {
		m.totpSecret, m.totpPending = secret, nil
	}
	m.totpLastStep = step
	m.updatedAt = now
	return nil
}

// ResetTOTP removes the secret; the member enrolls again at the next
// sign-in, and every session ends.
func (m *Member) ResetTOTP(now time.Time) {
	m.totpSecret, m.totpPending = nil, nil
	m.credentialsVersion++
	m.updatedAt = now
}

// Disable stops the member signing in and ends every session; Enable does
// not bring the sessions back.
func (m *Member) Disable(now time.Time) {
	m.status = StatusDisabled
	m.credentialsVersion++
	m.updatedAt = now
}

func (m *Member) Enable(now time.Time) {
	m.status = StatusActive
	m.updatedAt = now
}

// ManagedBy refuses to let anyone but the CLI change an admin.
func (m *Member) ManagedBy(a audit.Actor) error {
	if m.role == RoleAdmin && a.Kind != audit.ActorCLI {
		return ErrAdminManagedByCLI
	}
	return nil
}

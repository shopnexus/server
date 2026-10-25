package service

import (
	"net/netip"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// Me is the Me schema in api/schemas/staff.yaml.
type Me struct {
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
}

func meOf(m domain.Member) Me {
	return Me{ID: m.ID(), Email: m.Email(), DisplayName: m.DisplayName(), Role: m.Role()}
}

// MemberView is the Member schema.
type MemberView struct {
	ID           uuid.UUID     `json:"id"`
	Email        string        `json:"email"`
	DisplayName  string        `json:"display_name"`
	Role         domain.Role   `json:"role"`
	Status       domain.Status `json:"status"`
	TOTPEnrolled bool          `json:"totp_enrolled"`
	LockedUntil  *time.Time    `json:"locked_until,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

func viewOf(m domain.Member, lockedUntil *time.Time) MemberView {
	return MemberView{
		ID: m.ID(), Email: m.Email(), DisplayName: m.DisplayName(), Role: m.Role(), Status: m.Status(),
		TOTPEnrolled: m.Enrolled(), LockedUntil: lockedUntil, CreatedAt: m.CreatedAt(),
	}
}

// Enrollment is the TOTPEnrollment schema.
type Enrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

// LoginInput is one call to /v1/staff/login, from IP.
type LoginInput struct {
	Email       string
	Password    string
	NewPassword string
	Code        string
	UserAgent   string
	IP          netip.Addr
}

// SignedIn is a finished sign-in: the member, and the token and end of the
// session for the cookie.
type SignedIn struct {
	Me        Me
	Token     string
	ExpiresAt time.Time
}

// CreateMemberInput creates a member with Role, recorded for By.
type CreateMemberInput struct {
	Email       string
	DisplayName string
	Role        domain.Role
	By          audit.Actor
}

// AccountDetail is the StaffAccountDetail schema.
type AccountDetail struct {
	Account    accountsvc.Record  `json:"account"`
	Identities []authsvc.Identity `json:"identities"`
	Actions    []audit.Action     `json:"actions"`
}

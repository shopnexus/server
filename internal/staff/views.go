package staff

import (
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/staff/member"
)

// Me is the Me schema in api/schemas/staff.yaml.
type Me struct {
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Role        member.Role `json:"role"`
}

func meOf(m member.Member) Me {
	return Me{ID: m.ID(), Email: m.Email(), DisplayName: m.DisplayName(), Role: m.Role()}
}

// MemberView is the Member schema. It is also what an audit diff of a member
// shows, so it holds no secret.
type MemberView struct {
	ID           uuid.UUID     `json:"id"`
	Email        string        `json:"email"`
	DisplayName  string        `json:"display_name"`
	Role         member.Role   `json:"role"`
	Status       member.Status `json:"status"`
	TOTPEnrolled bool          `json:"totp_enrolled"`
	LockedUntil  *time.Time    `json:"locked_until,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

func viewOf(m member.Member, lockedUntil *time.Time) MemberView {
	return MemberView{
		ID: m.ID(), Email: m.Email(), DisplayName: m.DisplayName(), Role: m.Role(), Status: m.Status(),
		TOTPEnrolled: m.Enrolled(), LockedUntil: lockedUntil, CreatedAt: m.CreatedAt(),
	}
}

// LoginStep is the LoginStep schema.
type LoginStep struct {
	Ticket          string      `json:"ticket"`
	Next            member.Step `json:"next"`
	TicketExpiresAt time.Time   `json:"ticket_expires_at"`
}

// SignIn is the StaffSignIn schema.
type SignIn struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	TokenType            string    `json:"token_type"`
	Staff                Me        `json:"staff"`
}

// Access is the StaffAccess schema.
type Access struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	TokenType            string    `json:"token_type"`
}

// Enrollment is the TOTPEnrollment schema.
type Enrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

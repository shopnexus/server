package service

import (
	"net/netip"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
)

// Me is the Me schema in api/schemas/console.yaml.
type Me struct {
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
	// Dashboards are the ops dashboards and their floors, which GET
	// /v1/console/me carries so a moderator's sidebar follows a changed
	// floor.
	Dashboards []domain.DashboardFloor `json:"dashboards,omitempty"`
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
	// MustChangePassword is true while the member holds a temporary password.
	MustChangePassword bool       `json:"must_change_password"`
	LastSignInAt       *time.Time `json:"last_sign_in_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// viewOf leaves out LockedUntil and LastSignInAt, which ListMembers adds.
func viewOf(m domain.Member) MemberView {
	return MemberView{
		ID: m.ID(), Email: m.Email(), DisplayName: m.DisplayName(), Role: m.Role(), Status: m.Status(),
		TOTPEnrolled: m.Enrolled(), MustChangePassword: m.MustChangePassword(), CreatedAt: m.CreatedAt(),
	}
}

// Enrollment is the TOTPEnrollment schema.
type Enrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

// LoginInput is one call to /v1/console/login, from IP.
type LoginInput struct {
	Email       string
	Password    string
	NewPassword string
	Code        string
	UserAgent   string
	IP          netip.Addr
}

// SignedIn is a finished sign-in: the member, and the session's token for
// the cookie, which lasts domain.SessionTTL.
type SignedIn struct {
	Me    Me
	Token string
}

// CreateMemberInput creates a member with Role, recorded for By.
type CreateMemberInput struct {
	Email       string
	DisplayName string
	Role        domain.Role
	By          audit.Actor
}

// AccountDetail is the ConsoleAccountDetail schema.
type AccountDetail struct {
	Account    accountsvc.ConsoleAccount `json:"account"`
	Identities []accountsvc.Identity     `json:"identities"`
	Actions    []audit.Action            `json:"actions"`
}

// DashboardTicket is the DashboardTicket schema.
type DashboardTicket struct {
	Ticket      string `json:"ticket"`
	CallbackURL string `json:"callback_url"`
}

// DashboardOpened is a spent ticket: the dashboard cookie's token and how
// long the cookie lasts, which is what its console session has left.
type DashboardOpened struct {
	Token string
	TTL   time.Duration
}

// DashboardCheck is one forwardAuth call: the dashboard's host, the path and
// query asked for, and the console_dashboard cookie's value.
type DashboardCheck struct {
	Host  string
	URI   string
	Token string
}

// DashboardAccess answers a DashboardCheck. Granted carries the member's
// email and the role the dashboard reads; otherwise OpenURL is the
// console's page that asks for a ticket and comes back to the URI.
type DashboardAccess struct {
	Granted bool
	Email   string
	Role    string
	OpenURL string
}

// AccountQuery is what ListAccounts lists: every account, or the one Q names
// by ID or phone number, and with Suspended only accounts suspended now.
type AccountQuery struct {
	Q         string
	Suspended bool
}

// AccountList is the ConsoleAccountList schema. Total is set with
// AccountQuery.Suspended: how many accounts are suspended now, whatever Q
// and the page.
type AccountList struct {
	page.Page[accountsvc.ConsoleAccount]

	Total *int `json:"total,omitempty"`
}

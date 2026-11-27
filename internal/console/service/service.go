// Package service holds module console's use cases: signing moderators and
// admins in, managing members, and the customer accounts and audit log the
// console reads.
package service

import (
	"context"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
)

// Service is module console's contract: one method per use case of the
// console.
type Service interface {
	// Login signs a member in and starts a session.
	//
	// UC-MOD-10
	Login(ctx context.Context, in LoginInput) (SignedIn, error)
	// EnrollTOTP gives a member without TOTP a fresh secret.
	//
	// UC-MOD-10 A2
	EnrollTOTP(ctx context.Context, email, password string) (Enrollment, error)
	// Authenticate returns the live session whose cookie holds token.
	//
	// UC-MOD-10
	Authenticate(ctx context.Context, token string) (domain.Grant, error)
	// ActiveMember reads an active member by id, as a console agent's turn
	// names the member it works for; an unknown or disabled member is
	// authn.ErrUnauthenticated.
	ActiveMember(ctx context.Context, id uuid.UUID) (Me, error)
	// GetMe reads the signed-in member.
	//
	// UC-MOD-10
	GetMe(ctx context.Context, caller authn.ConsoleMember) (Me, error)
	// ChangePassword replaces the caller's password and ends every session.
	//
	// UC-MOD-10 A4
	ChangePassword(ctx context.Context, caller authn.ConsoleMember, current, next string) error
	// Logout signs the caller's session out.
	//
	// UC-MOD-10 A4
	Logout(ctx context.Context, caller authn.ConsoleMember) error
	// ListMembers lists members newest first, with their sign-in locks.
	//
	// UC-MOD-04
	ListMembers(ctx context.Context, p page.Request) (page.Page[MemberView], error)
	// FindMember reads the member with email, as cmd/console names one.
	FindMember(ctx context.Context, email string) (MemberView, error)
	// CreateMember creates a member and returns its temporary password once.
	//
	// UC-MOD-04 · UC-SHR-02
	CreateMember(ctx context.Context, in CreateMemberInput) (MemberView, string, error)
	// DisableMember and the five after it change a moderator, as an admin or
	// cmd/console; an admin only through cmd/console.
	//
	// UC-MOD-04 · BR-MOD-04 · UC-SHR-02
	DisableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	EnableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	UnlockMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	ResetMemberPassword(ctx context.Context, id uuid.UUID, by audit.Actor) (string, error)
	ResetMemberTOTP(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	RenameMember(ctx context.Context, id uuid.UUID, displayName string, by audit.Actor) (MemberView, error)
	// ListAccounts lists customer accounts, or the one in.Q names; with
	// in.Suspended, only those suspended now, with their count.
	//
	// UC-MOD-02
	ListAccounts(ctx context.Context, in AccountQuery, p page.Request) (AccountList, error)
	// GetAccount reads a customer account with its identities and history.
	//
	// UC-MOD-02 · UC-MOD-05 A1
	GetAccount(ctx context.Context, id uuid.UUID) (AccountDetail, error)
	// SuspendAccount and UnsuspendAccount suspend a customer account and
	// lift the suspension.
	//
	// UC-MOD-02 · BR-MOD-02 · UC-SHR-02
	SuspendAccount(ctx context.Context, in accountsvc.SuspendInput) (accountsvc.ConsoleAccount, error)
	UnsuspendAccount(ctx context.Context, id uuid.UUID, by audit.Actor) (accountsvc.ConsoleAccount, error)
	// ListActions lists the audit log newest first.
	//
	// UC-MOD-05
	ListActions(ctx context.Context, f audit.Filter, p page.Request) (page.Page[audit.Action], error)
	// CreateDashboardTicket gives the caller's session a ticket that opens
	// app once.
	//
	// UC-MOD-10 A3
	CreateDashboardTicket(ctx context.Context, caller authn.ConsoleMember, app domain.App) (DashboardTicket, error)
	// OpenDashboard spends a ticket at the dashboard's host and starts the
	// session its cookie holds.
	//
	// UC-MOD-10 A3
	OpenDashboard(ctx context.Context, host, ticket string) (DashboardOpened, error)
	// AuthorizeDashboard checks a request to a dashboard, for Traefik's
	// forwardAuth.
	//
	// UC-MOD-10 A3
	AuthorizeDashboard(ctx context.Context, in DashboardCheck) (DashboardAccess, error)
	// DashboardFloors lists the ops dashboards and the lowest role that
	// opens each.
	//
	// UC-MOD-10 A5
	DashboardFloors(ctx context.Context) ([]domain.DashboardFloor, error)
	// SetDashboardFloors changes dashboards' floors, as an admin.
	//
	// UC-MOD-10 A5 · UC-SHR-02
	SetDashboardFloors(ctx context.Context, caller authn.ConsoleMember, floors map[domain.App]domain.Role) ([]domain.DashboardFloor, error)
}

// Deps is what New builds Console from.
type Deps struct {
	Members  repository.Members
	Locks    repository.Locks
	Sessions repository.Sessions
	Actions  audit.Store
	Keys     Keys
	// Dashboards keeps the ops dashboards' tickets and sessions;
	// DashboardHosts maps each dashboard to its host (DASHBOARD_HOSTS) and
	// ConsoleOrigin is where a dashboard sends a reader without a session.
	Dashboards     repository.Dashboards
	DashboardHosts map[string]string
	ConsoleOrigin  string
	// SkipTOTP lets Login sign a member in with the password alone.
	SkipTOTP bool
	// Accounts reaches customer data; cmd/console, which manages only members,
	// leaves it nil.
	Accounts accountsvc.Service
	Now      func() time.Time
}

// Console implements Service in this process.
type Console struct{ d Deps }

var _ Service = (*Console)(nil)

func New(d Deps) *Console {
	return &Console{d: d}
}

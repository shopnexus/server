// Package service holds module console's use cases: signing moderators and
// admins in, managing members, and the customer accounts and audit log the
// console reads.
package service

import (
	"context"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/console/repository"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Service is module console's contract: one method per use case of the
// console.
type Service interface {
	// Login signs a member in and starts a session.
	Login(ctx context.Context, in LoginInput) (SignedIn, error)
	// EnrollTOTP gives a member without TOTP a fresh secret.
	EnrollTOTP(ctx context.Context, email, password string) (Enrollment, error)
	// Authenticate returns the live session whose cookie holds token.
	Authenticate(ctx context.Context, token string) (domain.Grant, error)
	// ActiveMember reads an active member by id, as the console agent's MCP
	// server names one; an unknown or disabled member is
	// authn.ErrUnauthenticated.
	ActiveMember(ctx context.Context, id uuid.UUID) (Me, error)
	// GetMe reads the signed-in member.
	GetMe(ctx context.Context, caller authn.ConsoleMember) (Me, error)
	// ChangePassword replaces the caller's password and ends every session.
	ChangePassword(ctx context.Context, caller authn.ConsoleMember, current, next string) error
	// Logout signs the caller's session out.
	Logout(ctx context.Context, caller authn.ConsoleMember) error
	// ListMembers lists members newest first, with their sign-in locks.
	ListMembers(ctx context.Context, p page.Request) (page.Page[MemberView], error)
	// FindMember reads the member with email, as cmd/console names one.
	FindMember(ctx context.Context, email string) (MemberView, error)
	// CreateMember creates a member and returns its temporary password once.
	CreateMember(ctx context.Context, in CreateMemberInput) (MemberView, string, error)
	DisableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	EnableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	UnlockMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	ResetMemberPassword(ctx context.Context, id uuid.UUID, by audit.Actor) (string, error)
	ResetMemberTOTP(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error)
	// ListAccounts lists customer accounts, or the one q names.
	ListAccounts(ctx context.Context, q string, p page.Request) (page.Page[accountsvc.ConsoleAccount], error)
	// GetAccount reads a customer account with its identities and history.
	GetAccount(ctx context.Context, id uuid.UUID) (AccountDetail, error)
	SuspendAccount(ctx context.Context, in accountsvc.SuspendInput) (accountsvc.ConsoleAccount, error)
	UnsuspendAccount(ctx context.Context, id uuid.UUID, by audit.Actor) (accountsvc.ConsoleAccount, error)
	// ListActions lists the audit log newest first.
	ListActions(ctx context.Context, f audit.Filter, p page.Request) (page.Page[audit.Action], error)
	// CreateDashboardTicket gives the caller's session a ticket that opens
	// app once.
	CreateDashboardTicket(ctx context.Context, caller authn.ConsoleMember, app domain.App) (DashboardTicket, error)
	// OpenDashboard spends a ticket at the dashboard's host and starts the
	// session its cookie holds.
	OpenDashboard(ctx context.Context, host, ticket string) (DashboardOpened, error)
	// AuthorizeDashboard checks a request to a dashboard, for Traefik's
	// forwardAuth.
	AuthorizeDashboard(ctx context.Context, in DashboardCheck) (DashboardAccess, error)
	DashboardLanding(ctx context.Context, in DashboardCheck) (string, error)
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
	// StoreLogin mints the keys the S3 console signs in with; nil leaves
	// its own sign-in page.
	StoreLogin func(context.Context) (domain.StoreCredentials, error)
	// Accounts reaches customer data; cmd/console, which manages only members,
	// leaves it nil.
	Accounts accountsvc.Service
	Now      func() time.Time
}

// Console implements Service in this process.
type Console struct {
	members  repository.Members
	locks    repository.Locks
	sessions repository.Sessions
	actions  audit.Store
	keys     Keys
	accounts accountsvc.Service
	now      func() time.Time

	dashboards     repository.Dashboards
	dashboardHosts map[string]string
	consoleOrigin  string
	storeLogin     func(context.Context) (domain.StoreCredentials, error)
}

var _ Service = (*Console)(nil)

func New(d Deps) *Console {
	return &Console{
		members: d.Members, locks: d.Locks, sessions: d.Sessions, actions: d.Actions, keys: d.Keys,
		accounts: d.Accounts, now: d.Now,
		dashboards: d.Dashboards, dashboardHosts: d.DashboardHosts, consoleOrigin: d.ConsoleOrigin,
		storeLogin: d.StoreLogin,
	}
}

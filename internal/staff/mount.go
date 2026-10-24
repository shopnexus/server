// Package staff signs in moderators and admins, checks their session and
// role on every request, and serves the console's API under /v1/staff.
// Package member holds the member entity; sessions and sign-in locks are
// plain stores in this package, and use cases are functions in it.
package staff

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/staff/member"
)

// Mount registers the staff endpoints. accounts and identities reach the
// customer data of modules account and auth.
func Mount(d module.Deps, k Keys, accounts AccountPort, identities IdentityPort) {
	st := NewStores(d.Pool, k)
	now, log := d.Now, d.Log
	console := FromConsole(log)
	handle := func(pattern string, h http.Handler) { d.Router.Handle(pattern, console(h)) }
	mod := Require(st.Sessions, now, log, member.RoleModerator)
	admin := Require(st.Sessions, now, log, member.RoleAdmin)

	handle("POST /v1/staff/login", LoginHandler(st, k, now, log))
	handle("POST /v1/staff/login/totp-enrollment", EnrollTOTPHandler(st, k, now, log))
	handle("POST /v1/staff/logout", mod(LogoutHandler(st.Sessions, now, log)))
	handle("GET /v1/staff/me", mod(GetMeHandler(st.Members, log)))
	handle("PUT /v1/staff/me/password", mod(ChangePasswordHandler(st.Members, k, now, log)))

	handle("GET /v1/staff/members", admin(ListMembersHandler(st, k, now, log)))
	handle("POST /v1/staff/members", admin(CreateMemberHandler(st.Members, k, now, log)))
	handle("POST /v1/staff/members/{id}/disable", admin(DisableMemberHandler(st.Members, now, log)))
	handle("POST /v1/staff/members/{id}/enable", admin(EnableMemberHandler(st.Members, now, log)))
	handle("POST /v1/staff/members/{id}/unlock", admin(UnlockMemberHandler(st, k, now, log)))
	handle("POST /v1/staff/members/{id}/reset-password", admin(ResetMemberPasswordHandler(st.Members, k, now, log)))
	handle("POST /v1/staff/members/{id}/reset-totp", admin(ResetMemberTOTPHandler(st.Members, now, log)))

	handle("GET /v1/staff/accounts", mod(ListAccountsHandler(accounts, identities, log)))
	handle("GET /v1/staff/accounts/{id}", mod(GetAccountHandler(accounts, identities, st.Actions, log)))
	handle("POST /v1/staff/accounts/{id}/suspend", mod(SuspendAccountHandler(accounts, log)))
	handle("POST /v1/staff/accounts/{id}/unsuspend", mod(UnsuspendAccountHandler(accounts, log)))
	handle("GET /v1/staff/actions", admin(ListActionsHandler(st.Actions, log)))
}

// Package staff signs in moderators and admins, checks their role on every
// request, and serves the console's API under /v1/staff. member, quota and
// session hold the aggregates; use cases are functions in this package.
package staff

import (
	"net/netip"

	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/staff/member"
)

// Settings is what staff's Mount takes beyond module.Deps.
type Settings struct {
	Keys           Keys
	TrustedProxies []netip.Prefix
}

// Mount registers the staff endpoints. accounts and identities reach the
// customer data of modules account and auth.
func Mount(d module.Deps, s Settings, accounts AccountPort, identities IdentityPort) {
	st := NewStores(d.Pool, s.Keys)
	r, k, trusted, now, log := d.Router, s.Keys, s.TrustedProxies, d.Now, d.Log
	r.Handle("POST /v1/staff/login", LoginHandler(st.Members, st.Quotas, k, trusted, now, log))
	r.Handle("POST /v1/staff/login/password", ChangeTemporaryPasswordHandler(st.Members, k, trusted, now, log))
	r.Handle("POST /v1/staff/login/totp-enrollment", EnrollTOTPHandler(st.Members, k, now, log))
	r.Handle("POST /v1/staff/login/totp", VerifyTOTPHandler(st, k, trusted, now, log))
	mod := Require(st.Sessions, k, trusted, now, log, member.RoleModerator)
	r.Handle("POST /v1/staff/session/refresh", RefreshHandler(st, k, trusted, now, log))
	r.Handle("POST /v1/staff/session/logout", mod(LogoutHandler(st.Sessions, now, log)))
	r.Handle("GET /v1/staff/me", mod(GetMeHandler(st.Members, log)))
	r.Handle("PUT /v1/staff/me/password", mod(ChangePasswordHandler(st, k, now, log)))
	admin := Require(st.Sessions, k, trusted, now, log, member.RoleAdmin)
	r.Handle("GET /v1/staff/members", admin(ListMembersHandler(st, k, now, log)))
	r.Handle("POST /v1/staff/members", admin(CreateMemberHandler(st, k, now, log)))
	r.Handle("POST /v1/staff/members/{id}/disable", admin(DisableMemberHandler(st, now, log)))
	r.Handle("POST /v1/staff/members/{id}/enable", admin(EnableMemberHandler(st, now, log)))
	r.Handle("POST /v1/staff/members/{id}/unlock", admin(UnlockMemberHandler(st, k, now, log)))
	r.Handle("POST /v1/staff/members/{id}/reset-password", admin(ResetMemberPasswordHandler(st, k, now, log)))
	r.Handle("POST /v1/staff/members/{id}/reset-totp", admin(ResetMemberTOTPHandler(st, now, log)))
	r.Handle("GET /v1/staff/accounts", mod(ListAccountsHandler(accounts, identities, log)))
	r.Handle("GET /v1/staff/accounts/{id}", mod(GetAccountHandler(accounts, identities, st.Actions, log)))
	r.Handle("POST /v1/staff/accounts/{id}/suspend", mod(SuspendAccountHandler(accounts, log)))
	r.Handle("POST /v1/staff/accounts/{id}/unsuspend", mod(UnsuspendAccountHandler(accounts, log)))
	r.Handle("GET /v1/staff/actions", admin(ListActionsHandler(st.Actions, log)))
}

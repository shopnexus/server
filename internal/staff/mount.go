// Package staff signs in moderators and admins, checks their role on every
// request, and serves the console's API under /v1/staff. member, quota and
// session hold the aggregates; use cases are functions in this package.
package staff

import (
	"net/netip"

	"github.com/shopnexus/server2/internal/platform/module"
)

// Settings is what staff's Mount takes beyond module.Deps.
type Settings struct {
	Keys           Keys
	TrustedProxies []netip.Prefix
}

// Mount registers the staff endpoints.
func Mount(d module.Deps, s Settings) {
	st := NewStores(d.Pool, s.Keys)
	r, k, trusted, now, log := d.Router, s.Keys, s.TrustedProxies, d.Now, d.Log
	r.Handle("POST /v1/staff/login", LoginHandler(st.Members, st.Quotas, k, trusted, now, log))
	r.Handle("POST /v1/staff/login/password", ChangeTemporaryPasswordHandler(st.Members, k, trusted, now, log))
	r.Handle("POST /v1/staff/login/totp-enrollment", EnrollTOTPHandler(st.Members, k, now, log))
	r.Handle("POST /v1/staff/login/totp", VerifyTOTPHandler(st, k, trusted, now, log))
}

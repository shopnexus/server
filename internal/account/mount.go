package account

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the account endpoints behind protect, which requires a
// signed-in caller, and returns the client other modules reach accounts
// through.
func Mount(d module.Deps, protect func(http.Handler) http.Handler) Client {
	accounts := NewPGStore(d.Pool)
	d.Router.Handle("GET /v1/accounts/me", protect(GetHandler(accounts, d.Now, d.Log)))
	d.Router.Handle("PATCH /v1/accounts/me", protect(RenameHandler(accounts, d.Now, d.Log)))
	return Client{Store: accounts, Now: d.Now}
}

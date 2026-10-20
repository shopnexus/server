package account

import (
	"log/slog"
	"time"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

// Deps holds what the package's use cases draw from. Only Routes and app.go
// read it; each use case declares its own fields.
type Deps struct {
	Store Store
	DB    postgres.Querier
	Tx    postgres.TxRunner
	Now   func() time.Time
	Log   *slog.Logger
}

// Routes registers the package's HTTP use cases. AccountCreate and
// AccountStatus have no route; app.go builds them for auth.
func Routes(r *httpx.Router, d Deps) {
	r.Handle("GET /v1/accounts/me", AccountGet{Store: d.Store, DB: d.DB, Now: d.Now}.Handler(d.Log))
	r.Handle("PATCH /v1/accounts/me", AccountRename{Store: d.Store, Tx: d.Tx, Now: d.Now}.Handler(d.Log))
}

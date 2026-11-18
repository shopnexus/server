// Package erasure deletes accounts: it asks deal for open orders and
// account to erase, and every module clears its own rows on
// account.deleted.
package erasure

import (
	accountsvc "github.com/shopnexus/server/internal/account/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/erasure/handler"
	"github.com/shopnexus/server/internal/erasure/service"
	"github.com/shopnexus/server/internal/platform/module"
)

// Mount registers DELETE /v1/accounts/me.
func Mount(d module.Deps, accounts accountsvc.Service, deals dealsvc.Service) {
	handler.New(service.New(service.Deps{Accounts: accounts, Deals: deals, Now: d.Now}), d.Log).Register(d.Router, d.Protect)
}

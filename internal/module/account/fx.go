package account

import (
	"shopnexus-server/config"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountecho "shopnexus-server/internal/module/account/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"

	"go.uber.org/fx"
)

// Module provides the account module dependencies
var Module = fx.Module("account",
	fx.Provide(
		NewAccountStorage,
		accountbiz.NewAccountBiz,
		NewAccountClient,
		accountecho.NewHandler,
	),
	fx.Invoke(
		accountecho.NewHandler,
	),
)

func NewAccountStorage(pool pgsqlc.TxBeginner) accountbiz.AccountStorage {
	return pgsqlc.NewStorage(pool, accountdb.New(pool))
}

func NewAccountClient(cfg *config.Config) accountbiz.AccountClient {
	return accountbiz.NewAccountBizProxy(cfg.Restate.IngressAddress)
}

package account

import (
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	accountecho "shopnexus-remastered/internal/module/account/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"

	"go.uber.org/fx"
)

// Module provides the account module dependencies
var Module = fx.Module("account",
	fx.Provide(
		NewAccountStorage,
		accountbiz.NewAccountBiz,
		accountecho.NewHandler,
	),
	fx.Invoke(
		accountecho.NewHandler,
	),
)

func NewAccountStorage(pool pgsqlc.TxBeginner) accountbiz.AccountStorage {
	return pgsqlc.NewStorage(pool, accountdb.New(pool))
}

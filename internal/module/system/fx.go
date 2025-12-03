package system

import (
	"go.uber.org/fx"

	systembiz "shopnexus-remastered/internal/module/system/biz"
	systemdb "shopnexus-remastered/internal/module/system/db/sqlc"
	systemecho "shopnexus-remastered/internal/module/system/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the system module dependencies
var Module = fx.Module("system",
	fx.Provide(
		NewSystemStorage,
		systembiz.NewSystemBiz,
		systemecho.NewHandler,
	),
	fx.Invoke(
		systemecho.NewHandler,
	),
)

func NewSystemStorage(pool pgsqlc.TxBeginner) systembiz.SystemStorage {
	return pgsqlc.NewStorage(pool, systemdb.New(pool))
}

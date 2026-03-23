package system

import (
	"go.uber.org/fx"

	systembiz "shopnexus-server/internal/module/system/biz"
	systemdb "shopnexus-server/internal/module/system/db/sqlc"
	systemecho "shopnexus-server/internal/module/system/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
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

// NewSystemStorage creates a new system storage backed by PostgreSQL.
func NewSystemStorage(pool pgsqlc.TxBeginner) systembiz.SystemStorage {
	return pgsqlc.NewStorage(pool, systemdb.New(pool))
}

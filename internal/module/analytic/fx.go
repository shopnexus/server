package analytic

import (
	"go.uber.org/fx"

	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	analyticecho "shopnexus-remastered/internal/module/analytic/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the analytic module dependencies
var Module = fx.Module("analytic",
	fx.Provide(
		NewAnalyticStorage,
		analyticbiz.NewAnalyticBiz,
		analyticecho.NewHandler,
	),
	fx.Invoke(
		analyticecho.NewHandler,
	),
)

func NewAnalyticStorage(pool pgsqlc.TxBeginner) analyticbiz.AnalyticStorage {
	return pgsqlc.NewStorage(pool, analyticdb.New(pool))
}

package analytic

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticecho "shopnexus-server/internal/module/analytic/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the analytic module dependencies
var Module = fx.Module("analytic",
	fx.Provide(
		NewAnalyticStorage,
		analyticbiz.NewAnalyticBiz,
		NewAnalyticClient,
		analyticecho.NewHandler,
	),
	fx.Invoke(
		analyticecho.NewHandler,
	),
)

// NewAnalyticStorage creates a new analytic storage backed by PostgreSQL.
func NewAnalyticStorage(pool pgsqlc.TxBeginner) analyticbiz.AnalyticStorage {
	return pgsqlc.NewStorage(pool, analyticdb.New(pool))
}

// NewAnalyticClient creates a Restate-backed client for the analytic module.
func NewAnalyticClient(cfg *config.Config) analyticbiz.AnalyticClient {
	return analyticbiz.NewAnalyticBizRestateClient(cfg.Restate.IngressAddress)
}

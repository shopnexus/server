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
		analyticbiz.NewAnalyticHandler,
		NewAnalyticBiz,
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

// NewAnalyticBiz creates a Restate-backed client for the analytic module.
func NewAnalyticBiz(cfg *config.Config) analyticbiz.AnalyticBiz {
	return analyticbiz.NewAnalyticRestateClient(cfg.Restate.IngressAddress)
}

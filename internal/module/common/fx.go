package common

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonecho "shopnexus-server/internal/module/common/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the common module dependencies.
var Module = fx.Module("common",
	fx.Provide(
		NewCommonStorage,
		commonbiz.NewcommonBiz,
		NewCommonBiz,
		commonecho.NewHandler,
	),
	fx.Invoke(
		commonecho.NewHandler,
	),
)

// NewCommonStorage creates a new common storage backed by PostgreSQL.
func NewCommonStorage(pool pgsqlc.TxBeginner) commonbiz.CommonStorage {
	return pgsqlc.NewStorage(pool, commondb.New(pool))
}

// NewCommonBiz creates a Restate-backed client for the common module.
func NewCommonBiz(cfg *config.Config) commonbiz.CommonBiz {
	return commonbiz.NewCommonRestateClient(cfg.Restate.IngressAddress)
}

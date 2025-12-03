package common

import (
	"go.uber.org/fx"

	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	commonecho "shopnexus-remastered/internal/module/common/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the common module dependencies
var Module = fx.Module("common",
	fx.Provide(
		NewCommonStorage,
		commonbiz.NewcommonBiz,
		commonecho.NewHandler,
	),
	fx.Invoke(
		commonecho.NewHandler,
	),
)

func NewCommonStorage(pool pgsqlc.TxBeginner) commonbiz.CommonStorage {
	return pgsqlc.NewStorage(pool, commondb.New(pool))
}

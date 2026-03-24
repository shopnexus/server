package common

import (
	"go.uber.org/fx"

	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonecho "shopnexus-server/internal/module/common/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the common module dependencies
var Module = fx.Module("common",
	fx.Provide(
		NewCommonStorage,
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

// NewCommonBiz creates a CommonBiz and provides it as the interface.
func NewCommonBiz(storage commonbiz.CommonStorage) (commonbiz.CommonBiz, error) {
	return commonbiz.NewcommonBiz(storage)
}

package catalog

import (
	"go.uber.org/fx"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogecho "shopnexus-remastered/internal/module/catalog/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the catalog module dependencies
var Module = fx.Module("catalog",
	fx.Provide(
		NewCatalogStorage,
		catalogbiz.NewCatalogBiz,
		catalogecho.NewHandler,
	),
	fx.Invoke(
		catalogecho.NewHandler,
	),
)

func NewCatalogStorage(pool pgsqlc.TxBeginner) catalogbiz.CatalogStorage {
	return pgsqlc.NewStorage(pool, catalogdb.New(pool))
}

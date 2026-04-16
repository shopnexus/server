package catalog

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogecho "shopnexus-server/internal/module/catalog/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the catalog module dependencies.
var Module = fx.Module("catalog",
	fx.Provide(
		NewCatalogStorage,
		catalogbiz.NewCatalogHandler,
		NewCatalogBiz,
		catalogecho.NewHandler,
	),
	fx.Invoke(
		catalogecho.NewHandler,
	),
)

// NewCatalogStorage creates a new catalog storage backed by PostgreSQL.
func NewCatalogStorage(pool pgsqlc.TxBeginner) catalogbiz.CatalogStorage {
	return pgsqlc.NewStorage(pool, catalogdb.New(pool))
}

// NewCatalogBiz creates a Restate-backed client for the catalog module.
func NewCatalogBiz(cfg *config.Config) catalogbiz.CatalogBiz {
	return catalogbiz.NewCatalogRestateClient(cfg.Restate.IngressAddress)
}

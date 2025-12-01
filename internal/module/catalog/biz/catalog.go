package catalogbiz

import (
	"net/http"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/pubsub"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	catalogdb "shopnexus-remastered/internal/module/catalog/db"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

type CatalogStorage = pgsqlc.Storage[*catalogdb.Queries]

type CatalogBiz struct {
	cache     cachestruct.Client
	pubsub    pubsub.Client
	storage   CatalogStorage
	common    *commonbiz.CommonBiz
	account   *accountbiz.AccountBiz
	inventory *inventorybiz.InventoryBiz
	promotion *promotionbiz.PromotionBiz

	searchClient *SearchClient
}

func NewCatalogBiz(
	config config.Config,
	pool pgsqlc.TxBeginner,
	cache cachestruct.Client,
	pubsub pubsub.Client,
	common *commonbiz.CommonBiz,
	account *accountbiz.AccountBiz,
	inventory *inventorybiz.InventoryBiz,
	promotion *promotionbiz.PromotionBiz,
) *CatalogBiz {
	return &CatalogBiz{
		cache:     cache,
		pubsub:    pubsub.Group("catalog"),
		storage:   pgsqlc.NewStorage(pool, catalogdb.New(pool)),
		common:    common,
		account:   account,
		inventory: inventory,
		promotion: promotion,

		searchClient: &SearchClient{
			searchServer: config.App.Search.Url,
			httpClient:   http.DefaultClient,
			batchSize:    config.App.Search.InteractionBatchSize,
			denseWeight:  config.App.Search.DenseWeight,
			sparseWeight: config.App.Search.SparseWeight,
		},
	}
}

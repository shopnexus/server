package catalogbiz

import (
	"context"
	"log/slog"
	"sync"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/embedding"
	"shopnexus-remastered/internal/infras/milvus"
	"shopnexus-remastered/internal/infras/pubsub"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
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
	analytic  *analyticbiz.AnalyticBiz

	// Vector search (replaces searchClient)
	milvus       *milvus.Client
	embedding    *embedding.Client
	denseWeight  float32
	sparseWeight float32
	batchSize    int

	// Event buffering (moved from SearchClient)
	mu       sync.Mutex
	buffer   []analyticmodel.Interaction
	syncLock sync.Mutex
}

func NewCatalogBiz(
	cfg *config.Config,
	storage CatalogStorage,
	cache cachestruct.Client,
	pubsub pubsub.Client,
	common *commonbiz.CommonBiz,
	account *accountbiz.AccountBiz,
	inventory *inventorybiz.InventoryBiz,
	promotion *promotionbiz.PromotionBiz,
	analytic *analyticbiz.AnalyticBiz,
	milvusClient *milvus.Client,
	embeddingClient *embedding.Client,
) *CatalogBiz {

	b := &CatalogBiz{
		cache:     cache,
		pubsub:    pubsub.Group("catalog"),
		storage:   storage,
		common:    common,
		account:   account,
		inventory: inventory,
		promotion: promotion,
		analytic:  analytic,

		milvus:       milvusClient,
		embedding:    embeddingClient,
		denseWeight:  cfg.App.Search.DenseWeight,
		sparseWeight: cfg.App.Search.SparseWeight,
		batchSize:    cfg.App.Search.InteractionBatchSize,
	}

	// Setup Milvus collections
	if err := b.SetupMilvusCollections(context.Background()); err != nil {
		slog.Error("Failed to setup Milvus collections", "error", err)
	}

	b.InitPubsub()
	b.SetupCron()

	return b
}

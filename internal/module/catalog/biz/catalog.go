package catalogbiz

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/cachestruct"
	"shopnexus-server/internal/infras/embedding"
	"shopnexus-server/internal/infras/milvus"
	"shopnexus-server/internal/infras/pubsub"
	accountbiz "shopnexus-server/internal/module/account/biz"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commonbiz "shopnexus-server/internal/module/common/biz"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	"shopnexus-server/internal/shared/pgsqlc"
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

func (b *CatalogBiz) WithTx(ctx context.Context, fn func(context.Context, *CatalogBiz) error) error {
	storage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer storage.Rollback(ctx)

	biz := &CatalogBiz{
		cache:        b.cache,
		pubsub:       b.pubsub,
		storage:      storage,
		common:       b.common,
		account:      b.account,
		inventory:    b.inventory,
		promotion:    b.promotion,
		analytic:     b.analytic,
		milvus:       b.milvus,
		embedding:    b.embedding,
		denseWeight:  b.denseWeight,
		sparseWeight: b.sparseWeight,
		batchSize:    b.batchSize,
	}

	if err = fn(ctx, biz); err != nil {
		return fmt.Errorf("failed to execute function with transaction: %w", err)
	}

	if err = storage.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

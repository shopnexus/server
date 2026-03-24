package catalogbiz

import (
	"context"
	"log/slog"
	"sync"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/cachestruct"
	"shopnexus-server/internal/infras/embedding"
	"shopnexus-server/internal/infras/milvus"
	restateclient "shopnexus-server/internal/infras/restate"
	accountbiz "shopnexus-server/internal/module/account/biz"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"
)

// CatalogBiz is the client interface for CatalogBizImpl, which is used by other modules to call CatalogBizImpl methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface CatalogBiz -service CatalogBiz
type CatalogBiz interface {
	// Product Detail
	GetProductDetail(ctx context.Context, params GetProductDetailParams) (catalogmodel.ProductDetail, error)

	// Product Card
	GetProductCard(ctx context.Context, params GetProductCardParams) (*catalogmodel.ProductCard, error)
	ListProductCard(ctx context.Context, params ListProductCardParams) (sharedmodel.PaginateResult[catalogmodel.ProductCard], error)
	ListRecommendedProductCard(ctx context.Context, params ListRecommendedProductCardParams) ([]catalogmodel.ProductCard, error)

	// Product SPU
	GetProductSpu(ctx context.Context, params GetProductSpuParams) (catalogmodel.ProductSpu, error)
	ListProductSpu(ctx context.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[catalogmodel.ProductSpu], error)
	CreateProductSpu(ctx context.Context, params CreateProductSpuParams) (catalogmodel.ProductSpu, error)
	UpdateProductSpu(ctx context.Context, params UpdateProductSpuParams) (catalogmodel.ProductSpu, error)
	DeleteProductSpu(ctx context.Context, params DeleteProductSpuParams) error

	// Product SKU
	ListProductSku(ctx context.Context, params ListProductSkuParams) ([]catalogmodel.ProductSku, error)
	CreateProductSku(ctx context.Context, params CreateProductSkuParams) (catalogmodel.ProductSku, error)
	UpdateProductSku(ctx context.Context, params UpdateProductSkuParams) (catalogmodel.ProductSku, error)
	DeleteProductSku(ctx context.Context, params DeleteProductSkuParams) error

	// Comment
	ListComment(ctx context.Context, params ListCommentParams) (sharedmodel.PaginateResult[catalogmodel.Comment], error)
	CreateComment(ctx context.Context, params CreateCommentParams) (catalogmodel.Comment, error)
	UpdateComment(ctx context.Context, params UpdateCommentParams) (catalogmodel.Comment, error)
	DeleteComment(ctx context.Context, params DeleteCommentParams) error

	// Tag
	ListTag(ctx context.Context, params ListTagParams) (sharedmodel.PaginateResult[catalogdb.CatalogTag], error)
	GetTag(ctx context.Context, params GetTagParams) (catalogdb.CatalogTag, error)

	// Brand
	ListBrand(ctx context.Context, params ListBrandParams) (sharedmodel.PaginateResult[catalogdb.CatalogBrand], error)

	// Category
	ListCategory(ctx context.Context, params ListCategoryParams) (sharedmodel.PaginateResult[catalogdb.CatalogCategory], error)

	// Search
	Search(ctx context.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error)
	GetRecommendations(ctx context.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error)
}

type CatalogStorage = pgsqlc.Storage[*catalogdb.Queries]

// CatalogBizImpl implements the core business logic for the catalog module.
type CatalogBizImpl struct {
	cache         cachestruct.Client
	restateClient *restateclient.Client
	storage       CatalogStorage
	common        commonbiz.CommonBiz
	account       accountbiz.AccountBiz
	inventory     inventorybiz.InventoryBiz
	promotion     promotionbiz.PromotionBiz

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

// NewCatalogBiz creates a new CatalogBizImpl with the given dependencies.
func NewCatalogBiz(
	cfg *config.Config,
	storage CatalogStorage,
	cache cachestruct.Client,
	restateClient *restateclient.Client,
	common commonbiz.CommonBiz,
	account accountbiz.AccountBiz,
	inventory inventorybiz.InventoryBiz,
	promotion promotionbiz.PromotionBiz,
	milvusClient *milvus.Client,
	embeddingClient *embedding.Client,
) *CatalogBizImpl {

	b := &CatalogBizImpl{
		cache:         cache,
		restateClient: restateClient,
		storage:       storage,
		common:        common,
		account:       account,
		inventory:     inventory,
		promotion:     promotion,

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

	b.SetupCron()

	return b
}

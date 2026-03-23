package catalogbiz

import (
	"context"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// CatalogClient is the client interface for CatalogBiz, which is used by other modules to call CatalogBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface CatalogClient -service CatalogBiz
type CatalogClient interface {
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

package catalogbiz

import (
	restate "github.com/restatedev/sdk-go"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// CatalogBizService wraps CatalogBiz for Restate registration.
// All methods accept restate.Context and delegate to CatalogBiz with context.Context.
type CatalogBizService struct {
	biz *CatalogBiz
}

func NewCatalogBizService(biz *CatalogBiz) *CatalogBizService {
	return &CatalogBizService{biz: biz}
}

// Product Detail

func (s *CatalogBizService) GetProductDetail(ctx restate.Context, params GetProductDetailParams) (catalogmodel.ProductDetail, error) {
	return s.biz.GetProductDetail(ctx, params)
}

// Product Card

func (s *CatalogBizService) GetProductCard(ctx restate.Context, params GetProductCardParams) (*catalogmodel.ProductCard, error) {
	return s.biz.GetProductCard(ctx, params)
}

func (s *CatalogBizService) ListProductCard(ctx restate.Context, params ListProductCardParams) (sharedmodel.PaginateResult[catalogmodel.ProductCard], error) {
	return s.biz.ListProductCard(ctx, params)
}

func (s *CatalogBizService) ListRecommendedProductCard(ctx restate.Context, params ListRecommendedProductCardParams) ([]catalogmodel.ProductCard, error) {
	return s.biz.ListRecommendedProductCard(ctx, params)
}

// Product SPU

func (s *CatalogBizService) GetProductSpu(ctx restate.Context, params GetProductSpuParams) (catalogmodel.ProductSpu, error) {
	return s.biz.GetProductSpu(ctx, params)
}

func (s *CatalogBizService) ListProductSpu(ctx restate.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[catalogmodel.ProductSpu], error) {
	return s.biz.ListProductSpu(ctx, params)
}

func (s *CatalogBizService) CreateProductSpu(ctx restate.Context, params CreateProductSpuParams) (catalogmodel.ProductSpu, error) {
	return s.biz.CreateProductSpu(ctx, params)
}

func (s *CatalogBizService) UpdateProductSpu(ctx restate.Context, params UpdateProductSpuParams) (catalogmodel.ProductSpu, error) {
	return s.biz.UpdateProductSpu(ctx, params)
}

func (s *CatalogBizService) DeleteProductSpu(ctx restate.Context, params DeleteProductSpuParams) error {
	return s.biz.DeleteProductSpu(ctx, params)
}

// Product SKU

func (s *CatalogBizService) ListProductSku(ctx restate.Context, params ListProductSkuParams) ([]catalogmodel.ProductSku, error) {
	return s.biz.ListProductSku(ctx, params)
}

func (s *CatalogBizService) CreateProductSku(ctx restate.Context, params CreateProductSkuParams) (catalogmodel.ProductSku, error) {
	return s.biz.CreateProductSku(ctx, params)
}

func (s *CatalogBizService) UpdateProductSku(ctx restate.Context, params UpdateProductSkuParams) (catalogmodel.ProductSku, error) {
	return s.biz.UpdateProductSku(ctx, params)
}

func (s *CatalogBizService) DeleteProductSku(ctx restate.Context, params DeleteProductSkuParams) error {
	return s.biz.DeleteProductSku(ctx, params)
}

// Comment

func (s *CatalogBizService) ListComment(ctx restate.Context, params ListCommentParams) (sharedmodel.PaginateResult[catalogmodel.Comment], error) {
	return s.biz.ListComment(ctx, params)
}

func (s *CatalogBizService) CreateComment(ctx restate.Context, params CreateCommentParams) (catalogmodel.Comment, error) {
	return s.biz.CreateComment(ctx, params)
}

func (s *CatalogBizService) UpdateComment(ctx restate.Context, params UpdateCommentParams) (catalogmodel.Comment, error) {
	return s.biz.UpdateComment(ctx, params)
}

func (s *CatalogBizService) DeleteComment(ctx restate.Context, params DeleteCommentParams) error {
	return s.biz.DeleteComment(ctx, params)
}

// Tag

func (s *CatalogBizService) ListTag(ctx restate.Context, params ListTagParams) (sharedmodel.PaginateResult[catalogdb.CatalogTag], error) {
	return s.biz.ListTag(ctx, params)
}

func (s *CatalogBizService) GetTag(ctx restate.Context, params GetTagParams) (catalogdb.CatalogTag, error) {
	return s.biz.GetTag(ctx, params)
}

// Brand

func (s *CatalogBizService) ListBrand(ctx restate.Context, params ListBrandParams) (sharedmodel.PaginateResult[catalogdb.CatalogBrand], error) {
	return s.biz.ListBrand(ctx, params)
}

// Category

func (s *CatalogBizService) ListCategory(ctx restate.Context, params ListCategoryParams) (sharedmodel.PaginateResult[catalogdb.CatalogCategory], error) {
	return s.biz.ListCategory(ctx, params)
}

// Search

func (s *CatalogBizService) Search(ctx restate.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	return s.biz.Search(ctx, params)
}

func (s *CatalogBizService) GetRecommendations(ctx restate.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	return s.biz.GetRecommendations(ctx, params)
}

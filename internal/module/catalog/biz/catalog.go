package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/client/cachestruct"
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	searchbiz "shopnexus-remastered/internal/module/search/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type CatalogBiz struct {
	cache        cachestruct.Client
	pubsub       pubsub.Client
	storage      *pgutil.Storage
	promotionBiz *promotionbiz.PromotionBiz
	search       *searchbiz.SearchBiz
}

func NewCatalogBiz(
	cache cachestruct.Client,
	pubsub pubsub.Client,
	storage *pgutil.Storage,
	promotionBiz *promotionbiz.PromotionBiz,
	search *searchbiz.SearchBiz,
) *CatalogBiz {
	return &CatalogBiz{
		cache:        cache,
		pubsub:       pubsub.Group("catalog"),
		storage:      storage,
		promotionBiz: promotionBiz,
		search:       search,
	}
}

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Code       []string `validate:"omitempty,dive,min=1,max=100"`
	AccountID  []int64  `validate:"omitempty,dive,gt=0"`
	CategoryID []int64  `validate:"omitempty,dive,gt=0"`
	BrandID    []int64  `validate:"omitempty,dive,gt=0"`
	IsActive   []bool   `validate:"omitempty,dive"`
}

func (b *CatalogBiz) ListProductSpu(ctx context.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[db.CatalogProductSpu], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSpu]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogProductSpu(ctx, db.CountCatalogProductSpuParams{
		Code:       params.Code,
		AccountID:  params.AccountID,
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	spus, err := b.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.Offset()),
		Code:       params.Code,
		AccountID:  params.AccountID,
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSpu]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       spus,
	}, nil
}

type ListProductSkuParams struct {
	sharedmodel.PaginationParams
	SpuID      []int64 `validate:"omitempty,dive,gt=0"`
	Price      []int64 `validate:"omitempty,dive,gt=0"`
	PriceFrom  null.Int64
	PriceTo    null.Int64
	CanCombine []bool
}

func (b *CatalogBiz) ListProductSku(ctx context.Context, params ListProductSkuParams) (sharedmodel.PaginateResult[db.CatalogProductSku], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSku]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogProductSku(ctx, db.CountCatalogProductSkuParams{
		SpuID:      params.SpuID,
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	skus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		IDFrom:     params.GetCursorID(),
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.Offset()),
		SpuID:      params.SpuID,
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSku]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       skus,
	}, nil
}

type ListProductSkuAttributeParams struct {
	sharedmodel.PaginationParams
	Name []string `validate:"omitempty,dive,min=1,max=100"`
}

func (b *CatalogBiz) ListProductSkuAttribute(ctx context.Context, params ListProductSkuAttributeParams) (sharedmodel.PaginateResult[db.CatalogProductSkuAttribute], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSkuAttribute]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogProductSkuAttribute(ctx, db.CountCatalogProductSkuAttributeParams{
		Name: params.Name,
	})
	if err != nil {
		return zero, err
	}

	attrs, err := b.storage.ListCatalogProductSkuAttribute(ctx, db.ListCatalogProductSkuAttributeParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
		Name:   params.Name,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSkuAttribute]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       attrs,
	}, nil
}

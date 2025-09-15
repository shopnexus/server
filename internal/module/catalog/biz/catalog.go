package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type CatalogBiz struct {
	storage      *pgutil.Storage
	promotionBiz *promotionbiz.PromotionBiz
}

func NewCatalogBiz(storage *pgutil.Storage, promotionBiz *promotionbiz.PromotionBiz) *CatalogBiz {
	return &CatalogBiz{
		storage:      storage,
		promotionBiz: promotionBiz,
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
		Offset:     pgutil.Int32ToPgInt4(params.GetOffset()),
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
		Data:       spus,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
	}, nil
}

type ListProductSkuParams struct {
	sharedmodel.PaginationParams
	SpuID      []int64 `validate:"omitempty,dive,gt=0"`
	SpuIDFrom  null.Int64
	SpuIDTo    null.Int64
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
		SpuIDFrom:  pgutil.NullInt64ToPgInt8(params.SpuIDFrom),
		SpuIDTo:    pgutil.NullInt64ToPgInt8(params.SpuIDTo),
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	skus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.GetOffset()),
		SpuID:      params.SpuID,
		SpuIDFrom:  pgutil.NullInt64ToPgInt8(params.SpuIDFrom),
		SpuIDTo:    pgutil.NullInt64ToPgInt8(params.SpuIDTo),
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSku]{
		Data:       skus,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
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
		Offset: pgutil.Int32ToPgInt4(params.GetOffset()),
		Name:   params.Name,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSkuAttribute]{
		Data:       attrs,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
	}, nil
}

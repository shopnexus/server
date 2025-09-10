package catalogbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
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
	Code       []string
	AccountID  []int64
	CategoryID []int64
	BrandID    []int64
	IsActive   []bool
}

func (c *CatalogBiz) ListProductSpu(ctx context.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[db.CatalogProductSpu], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSpu]

	total, err := c.storage.CountCatalogProductSpu(ctx, db.CountCatalogProductSpuParams{
		Code:       params.Code,
		AccountID:  params.AccountID,
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	spus, err := c.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
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
	SpuID      []int64
	SpuIDFrom  *int64
	SpuIDTo    *int64
	Price      []int64
	PriceFrom  *int64
	PriceTo    *int64
	CanCombine []bool
}

func (c *CatalogBiz) ListProductSku(ctx context.Context, params ListProductSkuParams) (sharedmodel.PaginateResult[db.CatalogProductSku], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSku]

	total, err := c.storage.CountCatalogProductSku(ctx, db.CountCatalogProductSkuParams{
		SpuID:      params.SpuID,
		SpuIDFrom:  pgutil.PtrToPgtype(params.SpuIDFrom, pgutil.Int64ToPgInt8),
		SpuIDTo:    pgutil.PtrToPgtype(params.SpuIDTo, pgutil.Int64ToPgInt8),
		Price:      params.Price,
		PriceFrom:  pgutil.PtrToPgtype(params.PriceFrom, pgutil.Int64ToPgInt8),
		PriceTo:    pgutil.PtrToPgtype(params.PriceTo, pgutil.Int64ToPgInt8),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	skus, err := c.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.GetOffset()),
		SpuID:      params.SpuID,
		SpuIDFrom:  pgutil.PtrToPgtype(params.SpuIDFrom, pgutil.Int64ToPgInt8),
		SpuIDTo:    pgutil.PtrToPgtype(params.SpuIDTo, pgutil.Int64ToPgInt8),
		Price:      params.Price,
		PriceFrom:  pgutil.PtrToPgtype(params.PriceFrom, pgutil.Int64ToPgInt8),
		PriceTo:    pgutil.PtrToPgtype(params.PriceTo, pgutil.Int64ToPgInt8),
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
	Name []string
}

func (c *CatalogBiz) ListProductSkuAttribute(ctx context.Context, params ListProductSkuAttributeParams) (sharedmodel.PaginateResult[db.CatalogProductSkuAttribute], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSkuAttribute]

	total, err := c.storage.CountCatalogProductSkuAttribute(ctx, db.CountCatalogProductSkuAttributeParams{
		Name: params.Name,
	})
	if err != nil {
		return zero, err
	}

	attrs, err := c.storage.ListCatalogProductSkuAttribute(ctx, db.ListCatalogProductSkuAttributeParams{
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

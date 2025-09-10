package promotionbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
)

type PromotionBiz struct {
	storage *pgutil.Storage
}

func NewPromotionBiz(storage *pgutil.Storage) *PromotionBiz {
	return &PromotionBiz{
		storage,
	}
}

type GetPromotionParams struct {
	ID int64
}

func (s *PromotionBiz) GetPromotion(ctx context.Context, params GetPromotionParams) (db.PromotionBase, error) {
	promo, err := s.storage.GetPromotionBase(ctx, db.GetPromotionBaseParams{
		ID: pgutil.PtrToPgtype(&params.ID, pgutil.Int64ToPgInt8),
	})
	if err != nil {
		return db.PromotionBase{}, err
	}

	return promo, nil
}

type ListPromotionParams struct {
	sharedmodel.PaginationParams
}

func (s *PromotionBiz) ListPromotion(ctx context.Context, params ListPromotionParams) (sharedmodel.PaginateResult[db.PromotionBase], error) {
	var zero sharedmodel.PaginateResult[db.PromotionBase]

	total, err := s.storage.CountCatalogProductSku(ctx, db.CountCatalogProductSkuParams{})
	if err != nil {
		return zero, err
	}

	promos, err := s.storage.ListPromotionBase(ctx, db.ListPromotionBaseParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.GetOffset()),
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.PromotionBase]{
		Data:       promos,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
	}, nil
}

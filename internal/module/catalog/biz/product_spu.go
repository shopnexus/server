package catalogbiz

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	searchmodel "shopnexus-remastered/internal/module/search/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

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

type CreateProductSpuParams struct {
	Account     authmodel.AuthenticatedAccount
	CategoryID  int64  `validate:"required,gt=0"`
	BrandID     int64  `validate:"required,gt=0"`
	Name        string `validate:"required,min=1,max=200"`
	Description string `validate:"required,max=1000"`
}

func (b *CatalogBiz) CreateProductSpu(ctx context.Context, params CreateProductSpuParams) (db.CatalogProductSpu, error) {
	var zero db.CatalogProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	spu, err := txStorage.CreateDefaultCatalogProductSpu(ctx, db.CreateDefaultCatalogProductSpuParams{
		Code:        "generate slug here", // TODO: create function generate slug
		AccountID:   params.Account.ID,
		CategoryID:  params.CategoryID,
		BrandID:     params.BrandID,
		Name:        params.Name,
		Description: params.Description,
		IsActive:    true,
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return spu, nil
}

type UpdateProductSpuParams struct {
	Account       authmodel.AuthenticatedAccount
	ID            int64       `validate:"required,gt=0"`
	FeaturedSkuID null.Int64  `validate:"omitnil,gt=0"`
	CategoryID    null.Int64  `validate:"omitnil,gt=0"`
	BrandID       null.Int64  `validate:"omitnil,gt=0"`
	Name          null.String `validate:"omitnil,min=1,max=200"`
	Description   null.String `validate:"omitnil,max=1000"`
	IsActive      null.Bool   `validate:"omitnil"`
}

func (b *CatalogBiz) UpdateProductSpu(ctx context.Context, params UpdateProductSpuParams) (db.CatalogProductSpu, error) {
	var zero db.CatalogProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// TODO: check if the featured sku id belongs to the spu id

	var code null.String
	if params.Name.Valid {
		c := fmt.Sprintf("%s.%s", slug.Make(params.Name.String), uuid.NewString())
		code.SetValid(c)
	}

	// Update the product spu
	spu, err := txStorage.UpdateCatalogProductSpu(ctx, db.UpdateCatalogProductSpuParams{
		ID:            params.ID,
		Code:          pgutil.NullStringToPgText(code),
		FeaturedSkuID: pgutil.NullInt64ToPgInt8(params.FeaturedSkuID),
		CategoryID:    pgutil.NullInt64ToPgInt8(params.CategoryID),
		BrandID:       pgutil.NullInt64ToPgInt8(params.BrandID),
		Name:          pgutil.NullStringToPgText(params.Name),
		Description:   pgutil.NullStringToPgText(params.Description),
		IsActive:      pgutil.NullBoolToPgBool(params.IsActive),
		DateUpdated:   pgutil.TimeToPgTimestamptz(time.Now()),
	})
	if err != nil {
		return zero, err
	}

	// Prepare the search sync update
	updateSearchSyncArg := db.UpdateStaleSearchSyncParams{
		RefType:         searchmodel.RefTypeProduct,
		RefID:           params.ID,
		IsStaleMetadata: pgutil.BoolToPgBool(true),
	}

	// If the description is updated, we also need to update the embedding
	if params.Description.Valid {
		updateSearchSyncArg.IsStaleEmbedding = pgutil.BoolToPgBool(true)
	}

	// Mark the search sync as stale
	if err := txStorage.UpdateStaleSearchSync(ctx, updateSearchSyncArg); err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return spu, nil
}

type DeleteProductSpuParams struct {
	Account authmodel.AuthenticatedAccount
	ID      int64 `validate:"required,gt=0"`
}

func (b *CatalogBiz) DeleteProductSpu(ctx context.Context, params DeleteProductSpuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	if err := txStorage.DeleteCatalogProductSpu(ctx, db.DeleteCatalogProductSpuParams{
		ID: []int64{params.ID},
	}); err != nil {
		return err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

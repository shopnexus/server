package catalogbiz

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	searchmodel "shopnexus-remastered/internal/module/search/model"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
)

func (b *CatalogBiz) getCategoryName(ctx context.Context, categoryID int64) string {
	category, err := b.storage.GetCatalogCategory(ctx, db.GetCatalogCategoryParams{
		ID: pgutil.Int64ToPgInt8(categoryID),
	})
	if err != nil {
		return ""
	}
	return category.Name
}

func (b *CatalogBiz) getBrandName(ctx context.Context, brandID int64) string {
	brand, err := b.storage.GetCatalogBrand(ctx, db.GetCatalogBrandParams{
		ID: pgutil.Int64ToPgInt8(brandID),
	})
	if err != nil {
		return ""
	}
	return brand.Name
}

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Account    authmodel.AuthenticatedAccount
	ID         []int64  `validate:"omitempty,dive,gt=0"`
	Code       []string `validate:"omitempty,dive,min=1,max=100"`
	CategoryID []int64  `validate:"omitempty,dive,gt=0"`
	BrandID    []int64  `validate:"omitempty,dive,gt=0"`
	IsActive   []bool   `validate:"omitempty,dive"`
}

func (b *CatalogBiz) ListProductSpu(ctx context.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[catalogmodel.ProductSpu], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.ProductSpu]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogProductSpu(ctx, db.CountCatalogProductSpuParams{
		ID:   params.ID,
		Code: params.Code,
		// AccountID:  []int64{params.Account.ID},
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	dbSpus, err := b.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
		ID:     params.ID,
		Code:   params.Code,
		// AccountID:  []int64{params.Account.ID}, // TODO: uncomment this
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	spuIDs := slice.Map(dbSpus, func(spu db.CatalogProductSpu) int64 { return spu.ID })

	tagRefs, err := b.storage.ListCatalogProductSpuTag(ctx, db.ListCatalogProductSpuTagParams{
		SpuID: spuIDs,
	})
	if err != nil {
		return zero, err
	}
	tags, err := b.storage.ListCatalogTag(ctx, db.ListCatalogTagParams{
		ID: slice.Map(tagRefs, func(ref db.CatalogProductSpuTag) int64 { return ref.TagID }),
	})
	if err != nil {
		return zero, err
	}
	tagsMap := slice.GroupBySlice(tags, func(tag db.CatalogTag) (int64, string) { return tag.ID, tag.Tag })

	// Get first image of the product
	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.SharedResourceRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	resourcesMap := slice.GroupBySlice(resources, func(r db.ListSortedResourcesRow) (int64, db.ListSortedResourcesRow) { return r.RefID, r })

	// Calculate rating score
	ratings, err := b.storage.ListRating(ctx, db.ListRatingParams{
		RefType: db.CatalogCommentRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	ratingMap := slice.GroupBy(ratings, func(r db.ListRatingRow) (int64, db.ListRatingRow) { return r.RefID, r })

	var spus []catalogmodel.ProductSpu
	for _, spu := range dbSpus {
		spus = append(spus, catalogmodel.ProductSpu{
			ID:            spu.ID,
			Code:          spu.Code,
			Category:      b.getCategoryName(ctx, spu.CategoryID),
			Brand:         b.getBrandName(ctx, spu.BrandID),
			FeaturedSkuID: null.NewInt(spu.FeaturedSkuID.Int64, spu.FeaturedSkuID.Valid),
			Name:          spu.Name,
			Description:   spu.Description,
			IsActive:      spu.IsActive,
			DateCreated:   spu.DateCreated.Time,
			DateUpdated:   spu.DateUpdated.Time,
			Rating: catalogmodel.ProductRating{
				Score: ratingMap[spu.ID].Score,
				Total: ratingMap[spu.ID].Count,
			},
			Tags: slice.NonNil(tagsMap[spu.ID]),
			Resources: slice.Map(resourcesMap[spu.ID], func(r db.ListSortedResourcesRow) string {
				return sharedbiz.GetResourceURL(string(r.Provider), r.ObjectKey)
			}),
		})
	}

	return sharedmodel.PaginateResult[catalogmodel.ProductSpu]{
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
		Code:        GenerateSlug(params.Name),
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
		code.SetValid(GenerateSlug(params.Name.String))
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

func GenerateSlug(name string) string {
	return fmt.Sprintf("%s.%s", slug.Make(name), uuid.NewString())
}

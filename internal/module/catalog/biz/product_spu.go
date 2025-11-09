package catalogbiz

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/samber/lo"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	searchmodel "shopnexus-remastered/internal/module/search/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
)

func (b *CatalogBiz) mustGetTagsMap(ctx context.Context, spuID []int64) map[int64][]string { // map[spuID][]tag
	tags, err := b.storage.ListCatalogProductSpuTag(ctx, db.ListCatalogProductSpuTagParams{
		SpuID: spuID,
	})
	if err != nil {
		zero := map[int64][]string{}
		for _, id := range spuID {
			zero[id] = []string{}
		}
		return zero
	}
	return lo.GroupByMap(tags, func(tag db.CatalogProductSpuTag) (int64, string) { return tag.SpuID, tag.Tag })
}

// TODO: use join instead of spamming N+1 queries
func (b *CatalogBiz) mustGetCategory(ctx context.Context, categoryID int64) db.CatalogCategory {
	category, _ := b.storage.GetCatalogCategory(ctx, db.GetCatalogCategoryParams{
		ID: pgutil.Int64ToPgInt8(categoryID),
	})
	return category
}

// TODO: use join instead of spamming N+1 queries
func (b *CatalogBiz) mustGetBrand(ctx context.Context, brandID int64) db.CatalogBrand {
	brand, _ := b.storage.GetCatalogBrand(ctx, db.GetCatalogBrandParams{
		ID: pgutil.Int64ToPgInt8(brandID),
	})
	return brand
}

type ListProductSpuParams struct {
	commonmodel.PaginationParams
	Account    authmodel.AuthenticatedAccount
	ID         []int64  `validate:"omitempty,dive,gt=0"`
	Code       []string `validate:"omitempty,dive,min=1,max=100"`
	CategoryID []int64  `validate:"omitempty,dive,gt=0"`
	BrandID    []int64  `validate:"omitempty,dive,gt=0"`
	IsActive   []bool   `validate:"omitempty,dive"`
}

func (b *CatalogBiz) ListProductSpu(ctx context.Context, params ListProductSpuParams) (commonmodel.PaginateResult[catalogmodel.ProductSpu], error) {
	var zero commonmodel.PaginateResult[catalogmodel.ProductSpu]

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

	spuIDs := lo.Map(dbSpus, func(spu db.CatalogProductSpu, _ int) int64 { return spu.ID })

	// Calculate rating score
	ratings, err := b.storage.ListRating(ctx, db.ListRatingParams{
		RefType: db.CatalogCommentRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	ratingMap := lo.KeyBy(ratings, func(r db.ListRatingRow) int64 { return r.RefID })

	tagsMap := b.mustGetTagsMap(ctx, spuIDs)

	resourcesMap, err := b.common.GetResources(ctx, db.CommonResourceRefTypeProductSpu, spuIDs)
	if err != nil {
		return zero, err
	}

	var spus []catalogmodel.ProductSpu
	for _, spu := range dbSpus {
		spus = append(spus, catalogmodel.ProductSpu{
			ID:            spu.ID,
			Code:          spu.Code,
			Category:      b.mustGetCategory(ctx, spu.CategoryID),
			Brand:         b.mustGetBrand(ctx, spu.BrandID),
			FeaturedSkuID: pgutil.PgInt8ToNullInt64(spu.FeaturedSkuID),
			Name:          spu.Name,
			Description:   spu.Description,
			IsActive:      spu.IsActive,
			DateCreated:   spu.DateCreated.Time,
			DateUpdated:   spu.DateUpdated.Time,
			Rating: catalogmodel.ProductRating{
				Score: ratingMap[spu.ID].Score,
				Total: ratingMap[spu.ID].Count,
			},
			Tags:      slice.EnsureSlice(tagsMap[spu.ID]),
			Resources: resourcesMap[spu.ID],
		})
	}

	return commonmodel.PaginateResult[catalogmodel.ProductSpu]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       spus,
	}, nil
}

type CreateProductSpuParams struct {
	Storage     pgsqlc.Storage
	Account     authmodel.AuthenticatedAccount
	CategoryID  int64       `validate:"required,gt=0"`
	BrandID     int64       `validate:"required,gt=0"`
	Name        string      `validate:"required,min=1,max=200"`
	Description string      `validate:"required,max=1000"`
	IsActive    bool        `validate:"omitempty"`
	Tags        []string    `validate:"required,dive,min=1,max=100"`
	ResourceIDs []uuid.UUID `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateProductSpu(ctx context.Context, params CreateProductSpuParams) (catalogmodel.ProductSpu, error) {
	var zero catalogmodel.ProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		spu       db.CatalogProductSpu
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		spu, err = txStorage.CreateDefaultCatalogProductSpu(ctx, db.CreateDefaultCatalogProductSpuParams{
			Code:        GenerateSlug(params.Name),
			AccountID:   params.Account.ID,
			CategoryID:  params.CategoryID,
			BrandID:     params.BrandID,
			Name:        params.Name,
			Description: params.Description,
			IsActive:    params.IsActive,
		})
		if err != nil {
			return err
		}

		// Create tags
		if err := b.updateTags(ctx, updateTagsParams{
			Storage: txStorage,
			SpuID:   spu.ID,
			Tags:    params.Tags,
		}); err != nil {
			return err
		}

		// Create resources
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:     txStorage,
			Account:     params.Account,
			RefType:     db.CommonResourceRefTypeProductSpu,
			RefID:       spu.ID,
			ResourceIDs: params.ResourceIDs,
		})
		if err != nil {
			return err
		}

		// Create system search sync (TODO: should move to event)
		if _, err := txStorage.CreateDefaultSystemSearchSync(ctx, db.CreateDefaultSystemSearchSyncParams{
			RefType: searchmodel.RefTypeProduct,
			RefID:   spu.ID,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create product spu: %w", err)
	}

	tagsMap := b.mustGetTagsMap(ctx, []int64{spu.ID})

	return catalogmodel.ProductSpu{
		ID:            spu.ID,
		Code:          spu.Code,
		Category:      b.mustGetCategory(ctx, spu.CategoryID),
		Brand:         b.mustGetBrand(ctx, spu.BrandID),
		FeaturedSkuID: pgutil.PgInt8ToNullInt64(spu.FeaturedSkuID),
		Name:          spu.Name,
		Description:   spu.Description,
		IsActive:      spu.IsActive,
		DateCreated:   spu.DateCreated.Time,
		DateUpdated:   spu.DateUpdated.Time,
		Rating:        catalogmodel.ProductRating{},
		Tags:          slice.EnsureSlice(tagsMap[spu.ID]),
		Resources:     resources,
	}, nil
}

type UpdateProductSpuParams struct {
	Storage       pgsqlc.Storage
	Account       authmodel.AuthenticatedAccount
	ID            int64       `validate:"required,gt=0"`
	FeaturedSkuID null.Int64  `validate:"omitnil,gt=0"`
	CategoryID    null.Int64  `validate:"omitnil,gt=0"`
	BrandID       null.Int64  `validate:"omitnil,gt=0"`
	Name          null.String `validate:"omitnil,min=1,max=200"`
	Description   null.String `validate:"omitnil,max=1000"`
	IsActive      null.Bool   `validate:"omitnil"`
	Tags          []string    `validate:"required,dive,min=1,max=100"`
	ResourceIDs   []uuid.UUID `validate:"omitempty,dive"`
}

func (b *CatalogBiz) UpdateProductSpu(ctx context.Context, params UpdateProductSpuParams) (catalogmodel.ProductSpu, error) {
	var zero catalogmodel.ProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: check if the featured sku id belongs to the spu id

	var code null.String
	if params.Name.Valid {
		code.SetValid(GenerateSlug(params.Name.String))
	}

	var (
		spu       db.CatalogProductSpu
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error

		// Update the product spu
		spu, err = txStorage.UpdateCatalogProductSpu(ctx, db.UpdateCatalogProductSpuParams{
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
			return err
		}

		// Update tags
		if err := b.updateTags(ctx, updateTagsParams{
			Storage: txStorage,
			SpuID:   spu.ID,
			Tags:    params.Tags,
		}); err != nil {
			return err
		}

		// Update resources
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         txStorage,
			Account:         params.Account,
			RefType:         db.CommonResourceRefTypeProductSpu,
			RefID:           spu.ID,
			ResourceIDs:     params.ResourceIDs,
			EmptyResources:  true,
			DeleteResources: true,
		})
		if err != nil {
			return err
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
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update product spu: %w", err)
	}

	return catalogmodel.ProductSpu{
		ID:            spu.ID,
		Code:          spu.Code,
		Category:      b.mustGetCategory(ctx, spu.CategoryID),
		Brand:         b.mustGetBrand(ctx, spu.BrandID),
		FeaturedSkuID: pgutil.PgInt8ToNullInt64(spu.FeaturedSkuID),
		Name:          spu.Name,
		Description:   spu.Description,
		IsActive:      spu.IsActive,
		DateCreated:   spu.DateCreated.Time,
		DateUpdated:   spu.DateUpdated.Time,
		Rating:        catalogmodel.ProductRating{},
		Tags:          params.Tags,
		Resources:     resources,
	}, nil
}

type DeleteProductSpuParams struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount
	ID      int64 `validate:"required,gt=0"`
}

func (b *CatalogBiz) DeleteProductSpu(ctx context.Context, params DeleteProductSpuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		if err := txStorage.DeleteCatalogProductSpu(ctx, db.DeleteCatalogProductSpuParams{
			ID: []int64{params.ID},
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete product spu: %w", err)
	}

	return nil
}

type updateTagsParams struct {
	Storage pgsqlc.Storage
	SpuID   int64
	Tags    []string
}

func (b *CatalogBiz) updateTags(ctx context.Context, params updateTagsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		if err := txStorage.DeleteCatalogProductSpuTag(ctx, db.DeleteCatalogProductSpuTagParams{
			SpuID: []int64{params.SpuID},
		}); err != nil {
			return err
		}

		dbTags, err := txStorage.ListCatalogTag(ctx, db.ListCatalogTagParams{
			ID: params.Tags,
		})
		if err != nil {
			return err
		}
		var nonExistingTags []string
		for _, tag := range params.Tags {
			if !slices.Contains(lo.Map(dbTags, func(t db.CatalogTag, _ int) string { return t.ID }), tag) {
				nonExistingTags = append(nonExistingTags, tag)
			}
		}

		if len(nonExistingTags) > 0 {
			var args []db.CreateCopyDefaultCatalogTagParams
			for _, tag := range nonExistingTags {
				args = append(args, db.CreateCopyDefaultCatalogTagParams{
					ID:          tag,
					Description: "",
				})
			}
			if _, err := txStorage.CreateCopyDefaultCatalogTag(ctx, args); err != nil {
				return err
			}
		}

		var args []db.CreateCopyDefaultCatalogProductSpuTagParams
		for _, tag := range params.Tags {
			args = append(args, db.CreateCopyDefaultCatalogProductSpuTagParams{
				SpuID: params.SpuID,
				Tag:   tag,
			})
		}
		if _, err := txStorage.CreateCopyDefaultCatalogProductSpuTag(ctx, args); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to update tags for spu %d: %w", params.SpuID, err)
	}

	return nil
}

func GenerateSlug(name string) string {
	return fmt.Sprintf("%s.%s", slug.Make(name), uuid.NewString())
}

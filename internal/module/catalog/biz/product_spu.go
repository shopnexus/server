package catalogbiz

import (
	"context"
	"fmt"
	"slices"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/samber/lo"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/guregu/null/v6"
)

func (b *CatalogBiz) mustGetTagsMap(ctx context.Context, spuID []uuid.UUID) map[uuid.UUID][]string { // map[spuID][]tag
	tags, err := b.storage.Querier().ListProductSpuTag(ctx, catalogdb.ListProductSpuTagParams{
		SpuID: spuID,
	})
	if err != nil {
		zero := map[uuid.UUID][]string{}
		for _, id := range spuID {
			zero[id] = []string{}
		}
		return zero
	}
	return lo.GroupByMap(tags, func(tag catalogdb.CatalogProductSpuTag) (uuid.UUID, string) { return tag.SpuID, tag.Tag })
}

// TODO: use join instead of spamming N+1 queries
func (b *CatalogBiz) mustGetCategory(ctx context.Context, categoryID uuid.UUID) catalogdb.CatalogCategory {
	category, _ := b.storage.Querier().GetCategory(ctx, catalogdb.GetCategoryParams{
		ID: uuid.NullUUID{UUID: categoryID, Valid: true},
	})
	return category
}

// TODO: use join instead of spamming N+1 queries
func (b *CatalogBiz) mustGetBrand(ctx context.Context, brandID uuid.UUID) catalogdb.CatalogBrand {
	brand, _ := b.storage.Querier().GetBrand(ctx, catalogdb.GetBrandParams{
		ID: uuid.NullUUID{UUID: brandID, Valid: true},
	})
	return brand
}

func (b *CatalogBiz) GetProductSpu(ctx context.Context, id uuid.UUID) (catalogmodel.ProductSpu, error) {
	listSpu, err := b.ListProductSpu(ctx, ListProductSpuParams{
		ID: []uuid.UUID{id},
	})
	if err != nil {
		return catalogmodel.ProductSpu{}, fmt.Errorf("failed to get product spu: %w", err)
	}
	if len(listSpu.Data) == 0 {
		return catalogmodel.ProductSpu{}, sharedmodel.ErrEntityNotFound.Fmt("ProductSpu")
	}
	return listSpu.Data[0], nil
}

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Account    accountmodel.AuthenticatedAccount
	ID         []uuid.UUID `validate:"omitempty,dive"`
	Slug       []string    `validate:"omitempty,dive,min=1,max=100"`
	CategoryID []uuid.UUID `validate:"omitempty,dive"`
	BrandID    []uuid.UUID `validate:"omitempty,dive"`
	IsActive   []bool      `validate:"omitempty,dive"`
}

func (b *CatalogBiz) ListProductSpu(ctx context.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[catalogmodel.ProductSpu], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.ProductSpu]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listCountSpu, err := b.storage.Querier().ListCountProductSpu(ctx, catalogdb.ListCountProductSpuParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
		ID:     params.ID,
		Slug:   params.Slug,
		// AccountID:  []int64{params.Account.ID}, // TODO: uncomment this (filter by account only for vendor)
		CategoryID: params.CategoryID,
		BrandID:    params.BrandID,
		IsActive:   params.IsActive,
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(listCountSpu) > 0 {
		total.SetValid(listCountSpu[0].TotalCount)
	}

	dbSpus := lo.Map(listCountSpu, func(row catalogdb.ListCountProductSpuRow, _ int) catalogdb.CatalogProductSpu {
		return row.CatalogProductSpu
	})

	spuIDs := lo.Map(dbSpus, func(spu catalogdb.CatalogProductSpu, _ int) uuid.UUID { return spu.ID })
	// Calculate rating score
	ratings, err := b.storage.Querier().ListRating(ctx, catalogdb.ListRatingParams{
		RefType: catalogdb.CatalogCommentRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	ratingMap := lo.KeyBy(ratings, func(r catalogdb.ListRatingRow) uuid.UUID { return r.RefID })

	tagsMap := b.mustGetTagsMap(ctx, spuIDs)

	resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeProductSpu, spuIDs)
	if err != nil {
		return zero, err
	}

	var spus []catalogmodel.ProductSpu
	for _, spu := range dbSpus {
		spus = append(spus, catalogmodel.ProductSpu{
			ID:            spu.ID,
			AccountID:     spu.AccountID,
			Slug:          spu.Slug,
			Category:      b.mustGetCategory(ctx, spu.CategoryID),
			Brand:         b.mustGetBrand(ctx, spu.BrandID),
			FeaturedSkuID: spu.FeaturedSkuID,
			Name:          spu.Name,
			Description:   spu.Description,
			IsActive:      spu.IsActive,
			DateCreated:   spu.DateCreated,
			DateUpdated:   spu.DateUpdated,
			Rating: catalogmodel.ProductRating{
				Score: ratingMap[spu.ID].Score,
				Total: ratingMap[spu.ID].Count,
			},
			Tags:      tagsMap[spu.ID],
			Resources: resourcesMap[spu.ID],
		})
	}

	return sharedmodel.PaginateResult[catalogmodel.ProductSpu]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       spus,
	}, nil
}

type CreateProductSpuParams struct {
	Storage        CatalogStorage
	Account        accountmodel.AuthenticatedAccount
	CategoryID     uuid.UUID                           `validate:"required"`
	BrandID        uuid.UUID                           `validate:"required"`
	Name           string                              `validate:"required,min=1,max=200"`
	Description    string                              `validate:"required,max=10000"`
	IsActive       bool                                `validate:"omitempty"`
	Tags           []string                            `validate:"required,dive,min=1,max=100"`
	ResourceIDs    []uuid.UUID                         `validate:"omitempty,dive"`
	Specifications []catalogmodel.ProductSpecification `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateProductSpu(ctx context.Context, params CreateProductSpuParams) (catalogmodel.ProductSpu, error) {
	var zero catalogmodel.ProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		spu       catalogdb.CatalogProductSpu
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		var err error
		specsBytes, err := sonic.Marshal(params.Specifications)
		if err != nil {
			return err
		}

		spu, err = txStorage.Querier().CreateDefaultProductSpu(ctx, catalogdb.CreateDefaultProductSpuParams{
			Slug:           GenerateSlug(params.Name),
			AccountID:      params.Account.ID,
			CategoryID:     params.CategoryID,
			BrandID:        params.BrandID,
			Name:           params.Name,
			Description:    params.Description,
			IsActive:       params.IsActive,
			Specifications: specsBytes,
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
			// TODO: use message queue instead of sequential processing
			Storage:     pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			Account:     params.Account,
			RefType:     commondb.CommonResourceRefTypeProductSpu,
			RefID:       spu.ID,
			ResourceIDs: params.ResourceIDs,
		})
		if err != nil {
			return err
		}

		// Create system search sync (TODO: should move to event)
		// if _, err := txStorage.Querier().CreateDefaultSystemSearchSync(ctx, catalogdb.CreateDefaultSystemSearchSyncParams{
		// 	RefType: searchmodel.RefTypeProduct,
		// 	RefID:   spu.ID,
		// }); err != nil {
		// 	return err
		// }

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create product spu: %w", err)
	}

	tagsMap := b.mustGetTagsMap(ctx, []uuid.UUID{spu.ID})

	return catalogmodel.ProductSpu{
		ID:            spu.ID,
		Slug:          spu.Slug,
		Category:      b.mustGetCategory(ctx, spu.CategoryID),
		Brand:         b.mustGetBrand(ctx, spu.BrandID),
		FeaturedSkuID: spu.FeaturedSkuID,
		Name:          spu.Name,
		Description:   spu.Description,
		IsActive:      spu.IsActive,
		DateCreated:   spu.DateCreated,
		DateUpdated:   spu.DateUpdated,
		Rating:        catalogmodel.ProductRating{},
		Tags:          tagsMap[spu.ID],
		Resources:     resources,
	}, nil
}

type UpdateProductSpuParams struct {
	Storage        CatalogStorage
	Account        accountmodel.AuthenticatedAccount
	ID             uuid.UUID                           `validate:"required"`
	FeaturedSkuID  uuid.NullUUID                       `validate:"omitnil"`
	CategoryID     uuid.NullUUID                       `validate:"omitnil"`
	BrandID        uuid.NullUUID                       `validate:"omitnil"`
	Name           null.String                         `validate:"omitnil,min=1,max=200"`
	Description    null.String                         `validate:"omitnil,max=10000"`
	IsActive       null.Bool                           `validate:"omitnil"`
	Tags           []string                            `validate:"omitempty,dive,min=1,max=100"`
	ResourceIDs    []uuid.UUID                         `validate:"omitempty,dive"`
	Specifications []catalogmodel.ProductSpecification `validate:"omitempty,dive"`
}

func (b *CatalogBiz) UpdateProductSpu(ctx context.Context, params UpdateProductSpuParams) (catalogmodel.ProductSpu, error) {
	var zero catalogmodel.ProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// Ensure the featured SKU (if provided) belongs to the current SPU.
	if params.FeaturedSkuID.Valid {
		skus, err := b.storage.Querier().ListProductSku(ctx, catalogdb.ListProductSkuParams{
			ID: []uuid.UUID{params.FeaturedSkuID.UUID},
		})
		if err != nil {
			return zero, fmt.Errorf("failed to validate featured sku: %w", err)
		}
		if len(skus) == 0 || skus[0].SpuID != params.ID {
			return zero, fmt.Errorf("featured sku does not belong to product spu")
		}
	}

	var slug null.String
	if params.Name.Valid {
		slug.SetValid(GenerateSlug(params.Name.String))
	}

	var (
		spu       catalogdb.CatalogProductSpu
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		var err error

		specsBytes, err := sonic.Marshal(params.Specifications)
		if err != nil {
			return err
		}

		// Update the product spu
		spu, err = txStorage.Querier().UpdateProductSpu(ctx, catalogdb.UpdateProductSpuParams{
			ID:            params.ID,
			Slug:          slug,
			FeaturedSkuID: params.FeaturedSkuID,
			CategoryID:    params.CategoryID,
			BrandID:       params.BrandID,
			Name:          params.Name,
			Description:   params.Description,
			IsActive:      params.IsActive,
			// TODO: add handle update now in tool generate sql
			// DateUpdated:    time.Now(),
			Specifications: specsBytes,
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
			// TODO: use message queue instead of sequential processing
			Storage:     pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			Account:     params.Account,
			RefType:     commondb.CommonResourceRefTypeProductSpu,
			RefID:       spu.ID,
			ResourceIDs: params.ResourceIDs,
		})
		if err != nil {
			return err
		}

		// TODO: move to event queue
		// // Prepare the search sync update
		// updateSearchSyncArg := catalogdb.UpdateStaleSearchSyncParams{
		// 	RefType:         searchmodel.RefTypeProduct,
		// 	RefID:           params.ID,
		// 	IsStaleMetadata: null.BoolFrom(true),
		// }

		// // If the description is updated, we also need to update the embedding
		// if params.Description.Valid {
		// 	updateSearchSyncArg.IsStaleEmbedding = null.BoolFrom(true)
		// }

		// // Mark the search sync as stale
		// if err := txStorage.UpdateStaleSearchSync(ctx, updateSearchSyncArg); err != nil {
		// 	return err
		// }

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update product spu: %w", err)
	}

	return catalogmodel.ProductSpu{
		ID:            spu.ID,
		Slug:          spu.Slug,
		Category:      b.mustGetCategory(ctx, spu.CategoryID),
		Brand:         b.mustGetBrand(ctx, spu.BrandID),
		FeaturedSkuID: spu.FeaturedSkuID,
		Name:          spu.Name,
		Description:   spu.Description,
		IsActive:      spu.IsActive,
		DateCreated:   spu.DateCreated,
		DateUpdated:   spu.DateUpdated,
		Rating:        catalogmodel.ProductRating{},
		Tags:          params.Tags,
		Resources:     resources,
	}, nil
}

type DeleteProductSpuParams struct {
	Storage CatalogStorage
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

func (b *CatalogBiz) DeleteProductSpu(ctx context.Context, params DeleteProductSpuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		if err := txStorage.Querier().DeleteProductSpu(ctx, catalogdb.DeleteProductSpuParams{
			ID: []uuid.UUID{params.ID},
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
	Storage CatalogStorage
	SpuID   uuid.UUID
	Tags    []string
}

func (b *CatalogBiz) updateTags(ctx context.Context, params updateTagsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		if err := txStorage.Querier().DeleteProductSpuTag(ctx, catalogdb.DeleteProductSpuTagParams{
			SpuID: []uuid.UUID{params.SpuID},
		}); err != nil {
			return err
		}

		dbTags, err := txStorage.Querier().ListTag(ctx, catalogdb.ListTagParams{
			ID: params.Tags,
		})
		if err != nil {
			return err
		}

		var nonExistingTags []string
		for _, tag := range params.Tags {
			if !slices.Contains(lo.Map(dbTags, func(t catalogdb.CatalogTag, _ int) string { return t.ID }), tag) {
				nonExistingTags = append(nonExistingTags, tag)
			}
		}

		if len(nonExistingTags) > 0 {
			var args []catalogdb.CreateCopyDefaultTagParams
			for _, tag := range nonExistingTags {
				args = append(args, catalogdb.CreateCopyDefaultTagParams{
					ID: tag,
					// Description: "",
				})
			}
			if _, err := txStorage.Querier().CreateCopyDefaultTag(ctx, args); err != nil {
				return err
			}
		}

		var args []catalogdb.CreateCopyDefaultProductSpuTagParams
		for _, tag := range params.Tags {
			args = append(args, catalogdb.CreateCopyDefaultProductSpuTagParams{
				SpuID: params.SpuID,
				Tag:   tag,
			})
		}
		if _, err := txStorage.Querier().CreateCopyDefaultProductSpuTag(ctx, args); err != nil {
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

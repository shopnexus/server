package catalogbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/samber/lo"

	accountmodel "shopnexus-server/internal/module/account/model"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/guregu/null/v6"
)

func (b *CatalogBiz) mustGetTagsMap(ctx restate.Context, spuID []uuid.UUID) map[uuid.UUID][]string { // map[spuID][]tag
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
func (b *CatalogBiz) mustGetCategory(ctx restate.Context, categoryID uuid.UUID) catalogdb.CatalogCategory {
	category, _ := b.storage.Querier().GetCategory(ctx, catalogdb.GetCategoryParams{
		ID: uuid.NullUUID{UUID: categoryID, Valid: true},
	})
	return category
}

// TODO: use join instead of spamming N+1 queries
func (b *CatalogBiz) mustGetBrand(ctx restate.Context, brandID uuid.UUID) catalogdb.CatalogBrand {
	brand, _ := b.storage.Querier().GetBrand(ctx, catalogdb.GetBrandParams{
		ID: uuid.NullUUID{UUID: brandID, Valid: true},
	})
	return brand
}

type GetProductSpuParams struct {
	ID   uuid.NullUUID `validate:"omitnil"`
	Slug null.String   `validate:"omitnil"`
}

func (b *CatalogBiz) GetProductSpu(ctx restate.Context, params GetProductSpuParams) (catalogmodel.ProductSpu, error) {
	var (
		listSpu sharedmodel.PaginateResult[catalogmodel.ProductSpu]
		err     error
	)

	if params.ID.Valid {
		listSpu, err = b.ListProductSpu(ctx, ListProductSpuParams{
			ID: []uuid.UUID{params.ID.UUID},
		})
		if err != nil {
			return catalogmodel.ProductSpu{}, fmt.Errorf("get product spu: %w", err)
		}
	} else if params.Slug.Valid {
		listSpu, err = b.ListProductSpu(ctx, ListProductSpuParams{
			Slug: []string{params.Slug.String},
		})
		if err != nil {
			return catalogmodel.ProductSpu{}, fmt.Errorf("get product spu by slug: %w", err)
		}
	}

	if len(listSpu.Data) == 0 {
		return catalogmodel.ProductSpu{}, sharedmodel.ErrEntityNotFound.Fmt("ProductSpu")
	}
	return listSpu.Data[0], nil
}

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Account    accountmodel.AuthenticatedAccount `validate:"omitempty"`
	ID         []uuid.UUID                       `validate:"omitempty,dive"`
	Slug       []string                          `validate:"omitempty,dive"`
	CategoryID []uuid.UUID                       `validate:"omitempty,dive"`
	BrandID    []uuid.UUID                       `validate:"omitempty,dive"`
	IsActive   []bool                            `validate:"omitempty,dive"`
}

func (b *CatalogBiz) ListProductSpu(ctx restate.Context, params ListProductSpuParams) (sharedmodel.PaginateResult[catalogmodel.ProductSpu], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.ProductSpu]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listCountSpu, err := b.storage.Querier().ListCountProductSpu(ctx, catalogdb.ListCountProductSpuParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
		ID:     params.ID,
		Slug:   params.Slug,
		// AccountID:  []int64{params.Account.ID}, // TODO!: uncomment this (filter by account only for vendor)
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
		specs := []catalogmodel.ProductSpecification{}
		sonic.Unmarshal(spu.Specifications, &specs)

		m := b.dbToProductSpu(ctx, spu)
		m.Rating = catalogmodel.ProductRating{
			Score: ratingMap[spu.ID].Score,
			Total: ratingMap[spu.ID].Count,
		}
		m.Tags = tagsMap[spu.ID]
		m.Resources = resourcesMap[spu.ID]
		m.Specifications = specs
		spus = append(spus, m)
	}

	return sharedmodel.PaginateResult[catalogmodel.ProductSpu]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       spus,
	}, nil
}

type CreateProductSpuParams struct {
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

func (b *CatalogBiz) CreateProductSpu(ctx restate.Context, params CreateProductSpuParams) (catalogmodel.ProductSpu, error) {
	var zero catalogmodel.ProductSpu

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	specsBytes, err := sonic.Marshal(params.Specifications)
	if err != nil {
		return zero, fmt.Errorf("create product spu: %w", err)
	}

	spu, err := b.storage.Querier().CreateDefaultProductSpu(ctx, catalogdb.CreateDefaultProductSpuParams{
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
		return zero, fmt.Errorf("create product spu: %w", err)
	}

	// Create tags
	if err := b.updateTags(ctx, b.storage.Querier(), updateTagsParams{
		SpuID: spu.ID,
		Tags:  params.Tags,
	}); err != nil {
		return zero, fmt.Errorf("create product spu: %w", err)
	}

	// Create resources
	resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
		// TODO: use message queue instead of sequential processing
		Account:     params.Account,
		RefType:     commondb.CommonResourceRefTypeProductSpu,
		RefID:       spu.ID,
		ResourceIDs: params.ResourceIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("create product spu: %w", err)
	}

	// Create system search sync (TODO: should move to event)
	if _, err := b.storage.Querier().CreateDefaultSearchSync(ctx, catalogdb.CreateDefaultSearchSyncParams{
		RefType: catalogdb.CatalogSearchSyncRefTypeProductSpu,
		RefID:   spu.ID,
	}); err != nil {
		return zero, fmt.Errorf("create product spu: %w", err)
	}

	tagsMap := b.mustGetTagsMap(ctx, []uuid.UUID{spu.ID})

	m := b.dbToProductSpu(ctx, spu)
	m.Tags = tagsMap[spu.ID]
	m.Resources = resources
	m.Specifications = params.Specifications
	return m, nil
}

type UpdateProductSpuParams struct {
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

func (b *CatalogBiz) UpdateProductSpu(ctx restate.Context, params UpdateProductSpuParams) (catalogmodel.ProductSpu, error) {
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
			return zero, fmt.Errorf("validate featured sku: %w", err)
		}
		if len(skus) == 0 || skus[0].SpuID != params.ID {
			return zero, catalogmodel.ErrSkuNotBelongToSpu
		}
	}

	var slug null.String
	if params.Name.Valid {
		slug.SetValid(GenerateSlug(params.Name.String))
	}

	specsBytes, err := sonic.Marshal(params.Specifications)
	if err != nil {
		return zero, fmt.Errorf("update product spu: %w", err)
	}

	// FIRST STEP: Update the product spu
	spu, err := b.storage.Querier().UpdateProductSpu(ctx, catalogdb.UpdateProductSpuParams{
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
		return zero, fmt.Errorf("update product spu: %w", err)
	}

	// NEXT STEP: Update tags
	if err := b.updateTags(ctx, b.storage.Querier(), updateTagsParams{
		SpuID: spu.ID,
		Tags:  params.Tags,
	}); err != nil {
		return zero, fmt.Errorf("update product spu: %w", err)
	}

	// NEXT STEP: Mark the search sync as stale
	updateSearchSyncArg := catalogdb.UpdateStaleSearchSyncParams{
		RefType:         catalogdb.CatalogSearchSyncRefTypeProductSpu,
		RefID:           params.ID,
		IsStaleMetadata: null.BoolFrom(true),
	}

	// If the description is changed, we also need to update the embedding
	if params.Description.Valid {
		updateSearchSyncArg.IsStaleEmbedding = null.BoolFrom(true)
	}

	if err := b.storage.Querier().UpdateStaleSearchSync(ctx, updateSearchSyncArg); err != nil {
		return zero, fmt.Errorf("update product spu: %w", err)
	}

	// LAST STEP: Update resources
	// TODO: use message queue instead of sequential processing
	resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
		Account:     params.Account,
		RefType:     commondb.CommonResourceRefTypeProductSpu,
		RefID:       spu.ID,
		ResourceIDs: params.ResourceIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("update product spu: %w", err)
	}

	m := b.dbToProductSpu(ctx, spu)
	m.Tags = params.Tags
	m.Resources = resources
	m.Specifications = params.Specifications
	return m, nil
}

type DeleteProductSpuParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

func (b *CatalogBiz) DeleteProductSpu(ctx restate.Context, params DeleteProductSpuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.Querier().DeleteProductSpu(ctx, catalogdb.DeleteProductSpuParams{
		ID: []uuid.UUID{params.ID},
	}); err != nil {
		return fmt.Errorf("delete product spu: %w", err)
	}

	return nil
}

type updateTagsParams struct {
	SpuID uuid.UUID
	Tags  []string
}

// updateTags replaces all tags for the given SPU. It must be called within an existing transaction.
func (b *CatalogBiz) updateTags(ctx restate.Context, q *catalogdb.Queries, params updateTagsParams) error {
	if err := q.DeleteProductSpuTag(ctx, catalogdb.DeleteProductSpuTagParams{
		SpuID: []uuid.UUID{params.SpuID},
	}); err != nil {
		return fmt.Errorf("delete existing tags for spu %s: %w", params.SpuID, err)
	}

	if len(params.Tags) == 0 {
		return nil
	}

	dbTags, err := q.ListTag(ctx, catalogdb.ListTagParams{
		ID: params.Tags,
	})
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}

	var nonExistingTags []string
	existingTagSet := make(map[string]struct{}, len(dbTags))
	for _, t := range dbTags {
		existingTagSet[t.ID] = struct{}{}
	}
	for _, tag := range params.Tags {
		if _, exists := existingTagSet[tag]; !exists {
			nonExistingTags = append(nonExistingTags, tag)
		}
	}

	if len(nonExistingTags) > 0 {
		var args []catalogdb.CreateCopyDefaultTagParams
		for _, tag := range nonExistingTags {
			args = append(args, catalogdb.CreateCopyDefaultTagParams{
				ID: tag,
			})
		}
		if _, err := q.CreateCopyDefaultTag(ctx, args); err != nil {
			return fmt.Errorf("create tags: %w", err)
		}
	}

	var args []catalogdb.CreateCopyDefaultProductSpuTagParams
	for _, tag := range params.Tags {
		args = append(args, catalogdb.CreateCopyDefaultProductSpuTagParams{
			SpuID: params.SpuID,
			Tag:   tag,
		})
	}
	if _, err := q.CreateCopyDefaultProductSpuTag(ctx, args); err != nil {
		return fmt.Errorf("create product spu tags: %w", err)
	}

	return nil
}

// dbToProductSpu maps a DB CatalogProductSpu row to the model type.
// Callers should set Rating, Tags, Resources, and Specifications as needed.
func (b *CatalogBiz) dbToProductSpu(ctx restate.Context, spu catalogdb.CatalogProductSpu) catalogmodel.ProductSpu {
	return catalogmodel.ProductSpu{
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
	}
}

func GenerateSlug(name string) string {
	return fmt.Sprintf("%s.%s", slug.Make(name), uuid.NewString())
}

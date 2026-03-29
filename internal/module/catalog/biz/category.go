package catalogbiz

import (
	restate "github.com/restatedev/sdk-go"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListCategoryParams struct {
	sharedmodel.PaginationParams
	ID     []uuid.UUID `validate:"omitempty,dive,gt=0"`
	Search null.String `validate:"omitnil"`
}

// ListCategory returns paginated categories with optional ID filter and text search.
func (b *CatalogHandler) ListCategory(ctx restate.Context, params ListCategoryParams) (sharedmodel.PaginateResult[catalogdb.CatalogCategory], error) {
	var zero sharedmodel.PaginateResult[catalogdb.CatalogCategory]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbCategories, err := b.storage.Querier().SearchCategory(ctx, catalogdb.SearchCategoryParams{
		ID:     params.ID,
		Search: params.Search,
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(dbCategories) > 0 {
		total.SetValid(dbCategories[0].TotalCount)
	}

	return sharedmodel.PaginateResult[catalogdb.CatalogCategory]{
		PageParams: params.PaginationParams,
		Data: lo.Map(dbCategories, func(dbCategory catalogdb.SearchCategoryRow, _ int) catalogdb.CatalogCategory {
			return dbCategory.CatalogCategory
		}),
		Total: total,
	}, nil
}

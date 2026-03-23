package catalogbiz

import (
	"context"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListCategoryParams struct {
	commonmodel.PaginationParams
	ID     []uuid.UUID `validate:"omitempty,dive,gt=0"`
	Search null.String `validate:"omitnil"`
}

func (b *CatalogBiz) ListCategory(ctx context.Context, params ListCategoryParams) (commonmodel.PaginateResult[catalogdb.CatalogCategory], error) {
	var zero commonmodel.PaginateResult[catalogdb.CatalogCategory]

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

	return commonmodel.PaginateResult[catalogdb.CatalogCategory]{
		PageParams: params.PaginationParams,
		Data: lo.Map(dbCategories, func(dbCategory catalogdb.SearchCategoryRow, _ int) catalogdb.CatalogCategory {
			return dbCategory.CatalogCategory
		}),
		Total: total,
	}, nil
}

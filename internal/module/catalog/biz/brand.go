package catalogbiz

import (
	restate "github.com/restatedev/sdk-go"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListBrandParams struct {
	commonmodel.PaginationParams
	ID     []uuid.UUID `validate:"omitempty"`
	Search null.String `validate:"omitnil"`
}

// ListBrand returns paginated brands with optional ID filter and text search.
func (b *CatalogBiz) ListBrand(ctx restate.Context, params ListBrandParams) (commonmodel.PaginateResult[catalogdb.CatalogBrand], error) {
	var zero commonmodel.PaginateResult[catalogdb.CatalogBrand]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbBrands, err := b.storage.Querier().SearchBrand(ctx, catalogdb.SearchBrandParams{
		Limit:  params.Limit,
		Offset: params.Offset(),

		ID:     params.ID,
		Search: params.Search,
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(dbBrands) > 0 {
		total.SetValid(dbBrands[0].TotalCount)
	}

	return commonmodel.PaginateResult[catalogdb.CatalogBrand]{
		PageParams: params.PaginationParams,
		Data:       lo.Map(dbBrands, func(dbBrand catalogdb.SearchBrandRow, _ int) catalogdb.CatalogBrand { return dbBrand.CatalogBrand }),
		Total:      total,
	}, nil
}

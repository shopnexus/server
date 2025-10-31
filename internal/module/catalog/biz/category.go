package catalogbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListCategoryParams struct {
	sharedmodel.PaginationParams
}

func (b *CatalogBiz) ListCategory(ctx context.Context, params ListCategoryParams) (sharedmodel.PaginateResult[db.CatalogCategory], error) {
	var zero sharedmodel.PaginateResult[db.CatalogCategory]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogCategory(ctx, db.CountCatalogCategoryParams{})
	if err != nil {
		return zero, err
	}

	dbCategories, err := b.storage.ListCatalogCategory(ctx, db.ListCatalogCategoryParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogCategory]{

		PageParams: params.PaginationParams,
		Data:       dbCategories,
		Total:      null.IntFrom(total),
	}, nil
}

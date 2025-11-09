package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/guregu/null/v6"
)

type ListCategoryParams struct {
	commonmodel.PaginationParams
}

func (b *CatalogBiz) ListCategory(ctx context.Context, params ListCategoryParams) (commonmodel.PaginateResult[db.CatalogCategory], error) {
	var zero commonmodel.PaginateResult[db.CatalogCategory]

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

	return commonmodel.PaginateResult[db.CatalogCategory]{

		PageParams: params.PaginationParams,
		Data:       dbCategories,
		Total:      null.IntFrom(total),
	}, nil
}

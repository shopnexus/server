package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListBrandParams struct {
	commonmodel.PaginationParams
}

func (b *CatalogBiz) ListBrand(ctx context.Context, params ListBrandParams) (commonmodel.PaginateResult[db.CatalogBrand], error) {
	var zero commonmodel.PaginateResult[db.CatalogBrand]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogBrand(ctx, db.CountCatalogBrandParams{})
	if err != nil {
		return zero, err
	}

	dbBrands, err := b.storage.ListCatalogBrand(ctx, db.ListCatalogBrandParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	return commonmodel.PaginateResult[db.CatalogBrand]{

		PageParams: params.PaginationParams,
		Data:       dbBrands,
		Total:      null.IntFrom(total),
	}, nil
}

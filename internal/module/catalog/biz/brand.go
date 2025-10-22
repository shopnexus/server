package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListBrandParams struct {
	sharedmodel.PaginationParams
}

func (b *CatalogBiz) ListBrand(ctx context.Context, params ListBrandParams) (sharedmodel.PaginateResult[db.CatalogBrand], error) {
	var zero sharedmodel.PaginateResult[db.CatalogBrand]

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

	return sharedmodel.PaginateResult[db.CatalogBrand]{

		PageParams: params.PaginationParams,
		Data:       dbBrands,
		Total:      null.IntFrom(total),
	}, nil
}

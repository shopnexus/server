package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/guregu/null/v6"
)

type ListTagParams struct {
	commonmodel.PaginationParams
}

func (b *CatalogBiz) ListTag(ctx context.Context, params ListTagParams) (commonmodel.PaginateResult[db.CatalogTag], error) {
	var zero commonmodel.PaginateResult[db.CatalogTag]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogTag(ctx, db.CountCatalogTagParams{})
	if err != nil {
		return zero, err
	}

	dbTags, err := b.storage.ListCatalogTag(ctx, db.ListCatalogTagParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	return commonmodel.PaginateResult[db.CatalogTag]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       dbTags,
	}, nil
}

type GetTagParams struct {
	Account authmodel.AuthenticatedAccount
	Tag     string `validate:"required,min=1,max=100"`
}

func (b *CatalogBiz) GetTag(ctx context.Context, params GetTagParams) (db.CatalogTag, error) {
	var zero db.CatalogTag

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	tag, err := b.storage.GetCatalogTag(ctx, pgutil.StringToPgText(params.Tag))
	if err != nil {
		return zero, err
	}

	return tag, nil
}

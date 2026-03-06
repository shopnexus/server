package catalogbiz

import (
	"context"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListTagParams struct {
	commonmodel.PaginationParams
	Search null.String `validate:"omitnil,max=100"`
}

func (b *CatalogBiz) ListTag(ctx context.Context, params ListTagParams) (commonmodel.PaginateResult[catalogdb.CatalogTag], error) {
	var zero commonmodel.PaginateResult[catalogdb.CatalogTag]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listTag, err := b.storage.Querier().SearchTag(ctx, catalogdb.SearchTagParams{
		Search: params.Search,
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(listTag) > 0 {
		total.SetValid(listTag[0].TotalCount)
	}

	return commonmodel.PaginateResult[catalogdb.CatalogTag]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       lo.Map(listTag, func(row catalogdb.SearchTagRow, _ int) catalogdb.CatalogTag { return row.CatalogTag }),
	}, nil
}

type GetTagParams struct {
	Account accountmodel.AuthenticatedAccount
	Tag     string `validate:"required,min=1,max=100"`
}

func (b *CatalogBiz) GetTag(ctx context.Context, params GetTagParams) (catalogdb.CatalogTag, error) {
	var zero catalogdb.CatalogTag

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	tag, err := b.storage.Querier().GetTag(ctx, null.StringFrom(params.Tag))
	if err != nil {
		return zero, err
	}

	return tag, nil
}

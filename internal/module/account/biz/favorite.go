package accountbiz

import (
	"context"
	"fmt"

	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type AddFavoriteParams struct {
	Account accountmodel.AuthenticatedAccount
	SpuID   uuid.UUID `validate:"required"`
}

func (b *AccountBiz) AddFavorite(ctx context.Context, params AddFavoriteParams) (accountdb.AccountFavorite, error) {
	var zero accountdb.AccountFavorite

	// Check if already favorited
	existing, err := b.storage.Querier().GetFavorite(ctx, accountdb.GetFavoriteParams{
		AccountID: params.Account.ID,
		SpuID:     params.SpuID,
	})
	if err == nil {
		return existing, nil // Already favorited
	}

	result, err := b.storage.Querier().CreateDefaultFavorite(ctx, accountdb.CreateDefaultFavoriteParams{
		AccountID: params.Account.ID,
		SpuID:     params.SpuID,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to add favorite: %w", err)
	}

	return result, nil
}

type RemoveFavoriteParams struct {
	Account accountmodel.AuthenticatedAccount
	SpuID   uuid.UUID `validate:"required"`
}

func (b *AccountBiz) RemoveFavorite(ctx context.Context, params RemoveFavoriteParams) error {
	return b.storage.Querier().DeleteFavorite(ctx, accountdb.DeleteFavoriteParams{
		AccountID: params.Account.ID,
		SpuID:     params.SpuID,
	})
}

type ListFavoriteParams struct {
	Account accountmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

func (b *AccountBiz) ListFavorite(ctx context.Context, params ListFavoriteParams) (sharedmodel.PaginateResult[accountdb.AccountFavorite], error) {
	var zero sharedmodel.PaginateResult[accountdb.AccountFavorite]
	params.PaginationParams = params.Constrain()

	rows, err := b.storage.Querier().ListCountFavorite(ctx, accountdb.ListCountFavoriteParams{
		AccountID: []uuid.UUID{params.Account.ID},
		Limit:     null.Int32From(params.Limit.Int32),
		Offset:    params.Offset(),
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list favorites: %w", err)
	}

	favorites := make([]accountdb.AccountFavorite, len(rows))
	var total int64
	for i, row := range rows {
		favorites[i] = row.AccountFavorite
		total = row.TotalCount
	}

	return sharedmodel.PaginateResult[accountdb.AccountFavorite]{
		PageParams: params.PaginationParams,
		Data:       favorites,
		Total:      null.IntFrom(total),
	}, nil
}

type CheckFavoriteParams struct {
	Account accountmodel.AuthenticatedAccount
	SpuID   uuid.UUID `validate:"required"`
}

func (b *AccountBiz) CheckFavorite(ctx context.Context, params CheckFavoriteParams) (bool, error) {
	_, err := b.storage.Querier().GetFavorite(ctx, accountdb.GetFavoriteParams{
		AccountID: params.Account.ID,
		SpuID:     params.SpuID,
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

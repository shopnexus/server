package orderbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type GetCartParams struct {
	AccountID uuid.UUID `validate:"required"`
}

func (b *OrderBiz) GetCart(ctx context.Context, params GetCartParams) ([]catalogmodel.ProductSku, error) {
	cartItems, err := b.storage.Querier().ListCartItem(ctx, orderdb.ListCartItemParams{
		AccountID: []uuid.UUID{params.AccountID},
	})
	if err != nil {
		return nil, nil
	}

	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: lo.Map(cartItems, func(c orderdb.OrderCartItem, _ int) uuid.UUID { return c.SkuID }),
	})
	if err != nil {
		return nil, err
	}

	return skus, nil
}

type UpdateCartParams struct {
	Account accountmodel.AuthenticatedAccount

	SkuID         uuid.UUID  `validate:"required"`
	Quantity      null.Int64 `validate:"omitnil,min=0,max=1000"`
	DeltaQuantity null.Int64 `validate:"omitnil,min=1,max=1000"`
}

func (b *OrderBiz) UpdateCart(ctx context.Context, params UpdateCartParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var newQuantity int64

	if params.DeltaQuantity.Valid {
		cartItem, err := b.storage.Querier().GetCartItem(ctx, orderdb.GetCartItemParams{
			AccountID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			SkuID:     uuid.NullUUID{UUID: params.SkuID, Valid: true},
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		newQuantity = cartItem.Quantity + params.DeltaQuantity.Int64
	} else if params.Quantity.Valid {
		newQuantity = params.Quantity.Int64
	} else {
		return fmt.Errorf("either quantity or delta_quantity must be provided")
	}

	// If quantity = 0, remove cart item and return early
	if params.Quantity.Valid && params.Quantity.Int64 <= 0 {
		return b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
			AccountID: []uuid.UUID{params.Account.ID},
			SkuID:     []uuid.UUID{params.SkuID},
		})
	}

	return b.storage.Querier().UpdateCart(ctx, orderdb.UpdateCartParams{
		AccountID: params.Account.ID,
		SkuID:     params.SkuID,
		Quantity:  newQuantity,
	})
}

type ClearCartParams struct {
	Account accountmodel.AuthenticatedAccount
}

func (b *OrderBiz) ClearCart(ctx context.Context, params ClearCartParams) error {
	return b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
}

package orderbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type GetCartParams struct {
	AccountID uuid.UUID `validate:"required"`
}

func (b *OrderBiz) GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error) {
	cartItems, err := b.storage.Querier().ListCartItem(ctx, orderdb.ListCartItemParams{
		AccountID: []uuid.UUID{params.AccountID},
	})
	if err != nil {
		return nil, err
	}

	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: lo.Map(cartItems, func(c orderdb.OrderCartItem, _ int) uuid.UUID { return c.SkuID }),
	})
	if err != nil {
		return nil, err
	}
	skuMap := lo.SliceToMap(skus, func(s catalogmodel.ProductSku) (uuid.UUID, catalogmodel.ProductSku) {
		return s.ID, s
	})

	var items []ordermodel.CartItem
	for _, cartItem := range cartItems {
		sku := skuMap[cartItem.SkuID]

		var resource *commonmodel.Resource
		resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeProductSpu, []uuid.UUID{sku.SpuID})
		if err != nil {
			continue
		}
		if res, exists := resourcesMap[sku.SpuID]; exists && len(res) > 0 {
			resource = &res[0]
		}

		items = append(items, ordermodel.CartItem{
			SpuID:    sku.SpuID,
			Sku:      sku,
			Quantity: cartItem.Quantity,
			Resource: resource,
		})
	}

	return items, nil
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
		if err := b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
			AccountID: []uuid.UUID{params.Account.ID},
			SkuID:     []uuid.UUID{params.SkuID},
		}); err != nil {
			return err
		}
		b.analytic.TrackInteraction(params.Account, analyticmodel.EventRemoveFromCart, analyticdb.AnalyticInteractionRefTypeProduct, params.SkuID.String())
		return nil
	}

	if err := b.storage.Querier().UpdateCart(ctx, orderdb.UpdateCartParams{
		AccountID: params.Account.ID,
		SkuID:     params.SkuID,
		Quantity:  newQuantity,
	}); err != nil {
		return err
	}
	b.analytic.TrackInteraction(params.Account, analyticmodel.EventAddToCart, analyticdb.AnalyticInteractionRefTypeProduct, params.SkuID.String())
	return nil
}

type ClearCartParams struct {
	Account accountmodel.AuthenticatedAccount
}

func (b *OrderBiz) ClearCart(ctx context.Context, params ClearCartParams) error {
	return b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
		AccountID: []uuid.UUID{params.Account.ID},
	})
}

type ListCheckoutCartParams struct {
	Account        accountmodel.AuthenticatedAccount
	SkuIDs         []uuid.UUID   `validate:"omitempty,dive"`           // Select items in cart to checkout
	BuyNowSkuID    uuid.NullUUID `validate:"omitempty"`                // Instant checkout
	BuyNowQuantity null.Int64    `validate:"omitempty,min=1,max=1000"` // Instant checkout quantity
}

func (b *OrderBiz) ListCheckoutCart(ctx context.Context, params ListCheckoutCartParams) ([]ordermodel.CartItem, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	var results []ordermodel.CartItem

	// Handle Buy Now case
	if params.BuyNowSkuID.Valid {
		if !params.BuyNowQuantity.Valid {
			return nil, fmt.Errorf("buy now quantity must be provided")
		}

		skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{ID: []uuid.UUID{params.BuyNowSkuID.UUID}})
		if err != nil {
			return nil, err
		}

		if len(skus) > 0 {
			sku := skus[0]
			var resource *commonmodel.Resource
			if resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeProductSpu, []uuid.UUID{sku.SpuID}); err == nil {
				if res, exists := resourcesMap[sku.SpuID]; exists && len(res) > 0 {
					resource = &res[0]
				}
			}

			results = append(results, ordermodel.CartItem{
				SpuID:    sku.SpuID,
				Sku:      sku,
				Quantity: params.BuyNowQuantity.Int64,
				Resource: resource,
			})
		}
	} else {
		// Regular cart checkout
		cart, err := b.GetCart(ctx, GetCartParams{AccountID: params.Account.ID})
		if err != nil {
			return nil, err
		}

		results = lo.Filter(cart, func(item ordermodel.CartItem, _ int) bool {
			return lo.Contains(params.SkuIDs, item.Sku.ID)
		})

	}

	return results, nil
}

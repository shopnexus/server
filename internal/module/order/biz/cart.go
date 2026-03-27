package orderbiz

import (
	"database/sql"
	"errors"

	restate "github.com/restatedev/sdk-go"

	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

// GetCart returns all cart items for the given account with SKU details and product images.
func (b *OrderHandler) GetCart(ctx restate.Context, params GetCartParams) ([]ordermodel.CartItem, error) {
	cartItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderCartItem, error) {
		return b.storage.Querier().ListCartItem(ctx, orderdb.ListCartItemParams{
			AccountID: []uuid.UUID{params.AccountID},
		})
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

	return restate.Run(ctx, func(ctx restate.RunContext) ([]ordermodel.CartItem, error) {
		var items []ordermodel.CartItem
		for _, cartItem := range cartItems {
			sku := skuMap[cartItem.SkuID]

			var resource *commonmodel.Resource
			resourcesMap, err := b.common.GetResources(ctx, commonbiz.GetResourcesParams{
				RefType: commondb.CommonResourceRefTypeProductSpu,
				RefIDs:  []uuid.UUID{sku.SpuID},
			})
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
	})
}

// UpdateCart adds, updates, or removes a cart item and tracks the interaction.
func (b *OrderHandler) UpdateCart(ctx restate.Context, params UpdateCartParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	// Track which event type to send after the durable step
	eventType, err := restate.Run(ctx, func(ctx restate.RunContext) (string, error) {
		var newQuantity int64

		if params.DeltaQuantity.Valid {
			cartItem, err := b.storage.Querier().GetCartItem(ctx, orderdb.GetCartItemParams{
				AccountID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
				SkuID:     uuid.NullUUID{UUID: params.SkuID, Valid: true},
			})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return "", err
			}
			newQuantity = cartItem.Quantity + params.DeltaQuantity.Int64
		} else if params.Quantity.Valid {
			newQuantity = params.Quantity.Int64
		} else {
			return "", ordermodel.ErrQuantityParamRequired.Terminal()
		}

		// If quantity = 0, remove cart item and return early
		if params.Quantity.Valid && params.Quantity.Int64 <= 0 {
			if err := b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
				AccountID: []uuid.UUID{params.Account.ID},
				SkuID:     []uuid.UUID{params.SkuID},
			}); err != nil {
				return "", err
			}
			return analyticmodel.EventRemoveFromCart, nil
		}

		if err := b.storage.Querier().UpdateCart(ctx, orderdb.UpdateCartParams{
			AccountID: params.Account.ID,
			SkuID:     params.SkuID,
			Quantity:  newQuantity,
		}); err != nil {
			return "", err
		}
		return analyticmodel.EventAddToCart, nil
	})
	if err != nil {
		return err
	}

	restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: []analyticbiz.CreateInteraction{{
			Account:   params.Account,
			EventType: eventType,
			RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
			RefID:     params.SkuID.String(),
		}},
	})
	return nil
}

// ClearCart removes all items from the account's cart.
func (b *OrderHandler) ClearCart(ctx restate.Context, params ClearCartParams) error {
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		return b.storage.Querier().DeleteCartItem(ctx, orderdb.DeleteCartItemParams{
			AccountID: []uuid.UUID{params.Account.ID},
		})
	})
}

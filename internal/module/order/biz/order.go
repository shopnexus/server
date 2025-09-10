package paymentbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/client/vnpay"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

type OrderBiz struct {
	storage     *pgutil.Storage
	vnpayClient vnpay.Client
	promotion   *promotionbiz.PromotionBiz
}

func NewOrderBiz(storage *pgutil.Storage, vnpayClient vnpay.Client, promotion *promotionbiz.PromotionBiz) *OrderBiz {
	return &OrderBiz{
		storage:     storage,
		vnpayClient: vnpayClient,
		promotion:   promotion,
	}
}

type GetOrderParams = struct {
	Account authmodel.AuthenticatedAccount
	OrderID int64
}

func (s *OrderBiz) GetOrder(ctx context.Context, params GetOrderParams) (db.OrderBase, error) {
	return s.storage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(params.OrderID))
}

type ListOrdersParams struct {
	sharedmodel.PaginationParams
}

func (s *OrderBiz) ListOrders(ctx context.Context, params ListOrdersParams) (result sharedmodel.PaginateResult[db.OrderBase], err error) {
	storageParams := db.ListOrderBaseParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.GetOffset()),
	}

	// User only see their own payments
	//if params.Role == db.RoleUser {
	//	storageParams.UserID = &params.AccountID
	//}

	total, err := s.storage.CountOrderBase(ctx, db.CountOrderBaseParams{})
	if err != nil {
		return result, err
	}

	payments, err := s.storage.ListOrderBase(ctx, storageParams)
	if err != nil {
		return result, err
	}

	return sharedmodel.PaginateResult[db.OrderBase]{
		Data:       payments,
		Limit:      params.Limit,
		Page:       params.Page,
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(payments[len(payments)-1].ID),
	}, nil
}

type CreateOrderParams struct {
	Account     authmodel.AuthenticatedAccount
	Address     string
	OrderMethod db.OrderPaymentMethod
	SkuIDs      []int64
}

type CreateOrderResult struct {
	Order db.OrderBase
	Url   string
}

func (s *OrderBiz) CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error) {
	var zero CreateOrderResult

	// Start transaction
	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// Remove the checkout items from cart
	cartMap := make(map[int64]db.AccountCartItem) // map[skuID]cartItem
	cartItems, err := txStorage.RemoveCheckoutItem(ctx, db.RemoveCheckoutItemParams{
		CartID: params.Account.ID,
		SkuID:  params.SkuIDs,
	})
	if err != nil {
		return zero, err
	}
	if len(cartItems) != len(params.SkuIDs) {
		// Prevent duplicate skuIDs in params or some sku not found in cart
		return zero, fmt.Errorf("some sku not found in cart")
	}
	for _, item := range cartItems {
		cartMap[item.SkuID] = item
	}

	// Retrieve skus data
	skus, err := txStorage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		ID: params.SkuIDs,
	})
	if err != nil {
		return zero, err
	}
	if len(skus) != len(params.SkuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound
	}
	skuMap := make(map[int64]db.CatalogProductSku) // map[skuID]sku
	for _, sku := range skus {
		skuMap[sku.ID] = sku
	}

	// Caculate prices
	spus, err := txStorage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		ID: slice.Map(skus, func(s db.CatalogProductSku) int64 { return s.SpuID }),
	})
	if err != nil {
		return zero, err
	}
	spuMap := make(map[int64]*db.CatalogProductSpu) // map[spuID]spu
	for _, spu := range spus {
		spuMap[spu.ID] = &spu
	}

	priceMap, err := s.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
	if err != nil {
		return zero, err
	}
	totalPrice := int64(0)
	for _, skuID := range params.SkuIDs {
		totalPrice += priceMap[skuID].Price * cartMap[skuID].Quantity
	}

	// Create order
	order, err := txStorage.CreateDefaultOrderBase(ctx, db.CreateDefaultOrderBaseParams{
		AccountID:     params.Account.ID,
		PaymentMethod: db.OrderPaymentMethodEWallet,
		Status:        db.SharedStatusPending,
		Address:       params.Address,
	})
	if err != nil {
		return zero, err
	}

	// Create order items
	var createOrderItemArgs []db.CreateBatchOrderItemParams
	for _, skuID := range params.SkuIDs {
		if skuMap[skuID].CanCombine {
			createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
				OrderID:  order.ID,
				SkuID:    skuID,
				Quantity: cartMap[skuID].Quantity,
			})
		} else {
			for i := int64(0); i < cartMap[skuID].Quantity; i++ {
				createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
					OrderID:  order.ID,
					SkuID:    skuID,
					Quantity: 1,
				})
			}
		}
	}

	// Get available serial id and attach to order items
	serials, err := s.storage.GetAvailableProducts(ctx, params.SkuIDs)
	if err != nil {
		return zero, err
	}
	serialMap := make(map[int64][]int64) // map[skuID][]serialID
	for _, serial := range serials {
		serialMap[serial.SkuID] = append(serialMap[serial.SkuID], serial.ID)
	}

	// Batch create order items and create serials for each item
	var batchErr error
	var createOrderSerialArgs []db.CreateCopyDefaultOrderItemSerialParams
	txStorage.CreateBatchOrderItem(ctx, createOrderItemArgs).QueryRow(func(_ int, item db.OrderItem, err error) {
		if err != nil {
			batchErr = err
			return
		}

		for i := int64(0); i < item.Quantity; i++ {
			if len(serialMap[item.SkuID]) == 0 {
				spu, _ := txStorage.GetCatalogProductSpu(ctx, db.GetCatalogProductSpuParams{
					ID: pgutil.Int64ToPgInt8(skuMap[item.SkuID].SpuID),
				})
				batchErr = ordermodel.ErrOutOfStock.Fmt(spu.Name)
				return
			}
			serialID := serialMap[item.SkuID][0]
			serialMap[item.SkuID] = serialMap[item.SkuID][1:]

			createOrderSerialArgs = append(createOrderSerialArgs, db.CreateCopyDefaultOrderItemSerialParams{
				OrderItemID:     item.ID,
				ProductSerialID: serialID,
			})
		}
	})
	if batchErr != nil {
		return zero, batchErr
	}

	// Attach serials to order items
	if _, err = txStorage.CreateCopyDefaultOrderItemSerial(ctx, createOrderSerialArgs); err != nil {
		return zero, err
	}

	url, err := s.vnpayClient.CreateOrder(ctx, vnpay.CreateOrderParams{
		RefID:  order.ID,
		Amount: totalPrice,
		Info:   fmt.Sprintf("Order for order %d", order.ID),
	})
	if err != nil {
		return zero, err
	}

	// Rollback if purchase failed
	if err = txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return CreateOrderResult{Order: order, Url: url}, nil
}

//
//type UpdateOrderParams struct {
//	ID        int64
//	AccountID int64
//	Role      db.AccountType
//	Method    *db.OrderOrderMethod
//	Address   *string
//	Status    *db.Status
//}
//
//func (s *OrderBiz) UpdateOrder(ctx context.Context, params UpdateOrderParams) error {
//	txStorage, err := s.storage.Begin(ctx)
//	if err != nil {
//		return err
//	}
//	defer txStorage.Rollback(ctx)
//
//	getOrderParams := db.GetOrderParams{
//		ID:     params.ID,
//		Status: ptr.ToPtr(db.StatusPending),
//	}
//
//	// User only see their own payments
//	if params.Role == db.RoleUser {
//		getOrderParams.UserID = &params.AccountID
//	}
//
//	// Order must be pending
//	payment, err := txStorage.GetOrder(ctx, getOrderParams)
//	if err != nil {
//		return err
//	}
//
//	// If payment method is cash, address is required
//	if (params.Method == nil && payment.Method == db.OrderOrderMethodCash || params.Method != nil && *params.Method == db.OrderOrderMethodCash) &&
//		(params.Address == nil && payment.Address == "" || params.Address != nil && *params.Address == "") {
//		return fmt.Errorf("address is required for payment method %s", *params.Method)
//	}
//
//	// If params.Status is not nil and not admin, check if account (staff, ...) has permission to update status
//	if params.Status != nil && params.Role != db.RoleAdmin {
//		if ok, err := s.accountSvc.HasPermission(ctx, account.HasPermissionParams{
//			AccountID: params.AccountID,
//			Permissions: []db.Permission{
//				db.PermissionUpdateOrder,
//			},
//		}); err != nil {
//			return err
//		} else if !ok {
//			return fmt.Errorf("account %d does not have permission to update payment status", params.AccountID)
//		}
//	}
//
//	if err = txStorage.UpdateOrder(ctx, db.UpdateOrderParams{
//		ID:      params.ID,
//		Method:  params.Method,
//		Address: params.Address,
//		Status:  params.Status,
//	}); err != nil {
//		return err
//	}
//
//	if err = txStorage.Commit(ctx); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//type CancelOrderParams = struct {
//	UserID  int64
//	OrderID int64
//}
//
//func (s *OrderBiz) CancelOrder(ctx context.Context, params CancelOrderParams) error {
//	txStorage, err := s.storage.Begin(ctx)
//	if err != nil {
//		return err
//	}
//	defer txStorage.Rollback(ctx)
//
//	payment, err := txStorage.GetOrder(ctx, db.GetOrderParams{
//		ID:     params.OrderID,
//		UserID: &params.UserID,
//	})
//	if err != nil {
//		return err
//	}
//
//	// No need to check ownership as we already check it in GetOrder
//	// if payment.UserID != *params.UserID {
//	// 	return fmt.Errorf("payment %d not belong to user %d", params.OrderID, params.UserID)
//	// }
//
//	if payment.Status != db.StatusPending {
//		return fmt.Errorf("payment %d cannot be canceled", params.OrderID)
//	}
//
//	if err = txStorage.UpdateOrder(ctx, db.UpdateOrderParams{
//		ID:     params.OrderID,
//		Status: ptr.ToPtr(db.StatusCanceled),
//	}); err != nil {
//		return err
//	}
//
//	if err = txStorage.Commit(ctx); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//type CancelRefundParams = struct {
//	UserID   int64
//	RefundID int64
//}
//
//func (s *OrderBiz) CancelRefund(ctx context.Context, params CancelRefundParams) error {
//	txStorage, err := s.storage.BeginTx(ctx)
//	if err != nil {
//		return err
//	}
//	defer txStorage.Rollback(ctx)
//
//	refund, err := txStorage.GetRefund(ctx, db.GetRefundParams{
//		ID:     params.RefundID,
//		UserID: &params.UserID,
//	})
//	if err != nil {
//		return err
//	}
//
//	if refund.Status != db.StatusPending {
//		return fmt.Errorf("refund %d cannot be canceled", params.RefundID)
//	}
//
//	if err = txStorage.UpdateRefund(ctx, db.UpdateRefundParams{
//		ID:     params.RefundID,
//		UserID: &params.UserID,
//		Status: ptr.ToPtr(db.StatusCanceled),
//	}); err != nil {
//		return err
//	}
//
//	if err = txStorage.Commit(ctx); err != nil {
//		return err
//	}
//
//	return nil
//}

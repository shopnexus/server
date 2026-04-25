package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// ListBuyerRefunds returns paginated refunds owned by the requesting buyer.
func (b *OrderHandler) ListBuyerRefunds(
	ctx restate.Context,
	params ListBuyerRefundsParams,
) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	pagination := params.PaginationParams.Constrain()

	rows, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderRefund, error) {
		return b.storage.Querier().ListBuyerRefunds(ctx, orderdb.ListBuyerRefundsParams{
			AccountID:   params.BuyerID,
			OffsetCount: pagination.Offset().Int32,
			LimitCount:  pagination.Limit.Int32,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list buyer refunds", err)
	}

	data := make([]ordermodel.Refund, 0, len(rows))
	for _, r := range rows {
		data = append(data, mapRefund(r))
	}
	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: pagination,
		Data:       data,
	}, nil
}

// ListSellerRefunds returns paginated refunds raised against items the
// requesting seller fulfilled. The list is the seller's pending-action queue.
func (b *OrderHandler) ListSellerRefunds(
	ctx restate.Context,
	params ListSellerRefundsParams,
) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	pagination := params.PaginationParams.Constrain()

	rows, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderRefund, error) {
		return b.storage.Querier().ListSellerRefunds(ctx, orderdb.ListSellerRefundsParams{
			SellerID:    params.SellerID,
			OffsetCount: pagination.Offset().Int32,
			LimitCount:  pagination.Limit.Int32,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list seller refunds", err)
	}

	data := make([]ordermodel.Refund, 0, len(rows))
	for _, r := range rows {
		data = append(data, mapRefund(r))
	}
	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: pagination,
		Data:       data,
	}, nil
}

// CreateBuyerRefund creates a new 2-stage refund request for a paid order item.
// A return transport is created automatically; the buyer supplies the return method and address.
func (b *OrderHandler) CreateBuyerRefund(
	ctx restate.Context,
	params CreateBuyerRefundParams,
) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	// Validate item ownership, confirmation, and guard against duplicate active refunds.
	item, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderItem, error) {
		it, err := b.storage.Querier().GetItem(ctx, null.IntFrom(params.OrderItemID))
		if err != nil {
			return it, sharedmodel.WrapErr("get item", err)
		}
		if it.AccountID != params.Account.ID {
			return it, ordermodel.ErrItemNotOwnedByBuyer.Terminal()
		}
		if !it.OrderID.Valid {
			return it, ordermodel.ErrItemNotConfirmed.Terminal()
		}
		if it.DateCancelled.Valid {
			return it, ordermodel.ErrItemAlreadyCancelled.Terminal()
		}
		active, err := b.storage.Querier().HasActiveRefundForItem(ctx, params.OrderItemID)
		if err != nil {
			return it, sharedmodel.WrapErr("check active refund", err)
		}
		if active {
			return it, ordermodel.ErrRefundAlreadyAccepted.Terminal()
		}
		return it, nil
	})
	if err != nil {
		return zero, err
	}
	_ = item

	// Create a placeholder return transport (logistics filled in when seller accepts stage 1).
	returnTransport, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransport, error) {
		return b.storage.Querier().CreateDefaultTransport(ctx, orderdb.CreateDefaultTransportParams{
			Option: params.ReturnTransportOption,
			Data:   json.RawMessage(`{"direction":"return"}`),
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create return transport", err)
	}

	var addr null.String
	if params.Address != "" {
		addr = null.StringFrom(params.Address)
	}

	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().CreateDefaultRefund(ctx, orderdb.CreateDefaultRefundParams{
			AccountID:   params.Account.ID,
			OrderItemID: params.OrderItemID,
			TransportID: returnTransport.ID,
			Method:      params.Method,
			Reason:      params.Reason,
			Address:     addr,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create refund", err)
	}

	return mapRefund(refund), nil
}

// AcceptRefundStage1 is called by the seller to accept a Pending refund and move it to Processing.
// The seller provides shipping details for the buyer's return transport.
func (b *OrderHandler) AcceptRefundStage1(
	ctx restate.Context,
	params AcceptRefundStage1Params,
) (ordermodel.Refund, error) {
	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().AcceptRefundStage1(ctx, orderdb.AcceptRefundStage1Params{
			ID:           params.RefundID,
			AcceptedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
		})
	})
	if err != nil {
		return ordermodel.Refund{}, sharedmodel.WrapErr("accept stage 1", err)
	}
	return mapRefund(refund), nil
}

// ApproveRefundStage2 is called by the seller after the returned item is received and inspected.
// It creates a refund tx (Success), credits the buyer's wallet, cancels the item,
// and cancels the pending payout if the order had one.
func (b *OrderHandler) ApproveRefundStage2(
	ctx restate.Context,
	params ApproveRefundStage2Params,
) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get refund", err)
	}
	if refund.Status != orderdb.OrderStatusProcessing {
		return zero, ordermodel.ErrRefundStageSkipped.Terminal()
	}

	item, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderItem, error) {
		return b.storage.Querier().GetItem(ctx, null.IntFrom(refund.OrderItemID))
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get item", err)
	}
	if item.SellerID != params.Account.ID {
		return zero, ordermodel.ErrItemNotOwnedBySeller.Terminal()
	}

	// Infer buyer currency before the durable Run (outside Run — cross-module).
	buyerCurrency, err := b.inferCurrency(ctx, item.AccountID)
	if err != nil {
		return zero, sharedmodel.WrapErr("infer buyer currency", err)
	}

	// All mutations in one durable Run: create refund tx, approve refund row, cancel item, cancel payout.
	result, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		refundTx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
			FromID:        uuid.NullUUID{},
			ToID:          uuid.NullUUID{UUID: item.AccountID, Valid: true},
			Type:          TxTypeRefund,
			Status:        orderdb.OrderStatusSuccess,
			Note:          fmt.Sprintf("refund approved for item %d", item.ID),
			Amount:        item.PaidAmount,
			FromCurrency:  buyerCurrency,
			ToCurrency:    buyerCurrency,
			Data:          json.RawMessage("{}"),
			DatePaid:      null.TimeFrom(time.Now()),
			DateExpired:   time.Now(),
		})
		if err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("create refund tx", err)
		}

		updated, err := b.storage.Querier().ApproveRefundStage2(ctx, orderdb.ApproveRefundStage2Params{
			ID:           refund.ID,
			ApprovedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			RefundTxID:   null.IntFrom(refundTx.ID),
		})
		if err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("approve stage 2", err)
		}

		if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
			ID:            item.ID,
			CancelledByID: uuid.NullUUID{UUID: item.AccountID, Valid: true},
			RefundTxID:    null.IntFrom(refundTx.ID),
		}); err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("cancel item", err)
		}

		// Cancel pending payout if one exists for the order.
		if item.OrderID.Valid {
			if payout, err := b.storage.Querier().GetPendingPayoutTxForOrder(ctx, item.OrderID); err == nil {
				if _, err := b.storage.Querier().MarkTransactionCancelled(ctx, payout.ID); err != nil {
					return orderdb.OrderRefund{}, sharedmodel.WrapErr("cancel payout", err)
				}
			}
		}

		return updated, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("approve stage 2", err)
	}

	// Credit buyer's wallet outside Run (cross-module Restate call).
	if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
		AccountID: item.AccountID,
		Amount:    item.PaidAmount,
		Type:      "Refund",
		Reference: fmt.Sprintf("refund:%s", refund.ID),
		Note:      "refund approved",
	}); err != nil {
		return zero, sharedmodel.WrapErr("wallet credit buyer", err)
	}

	return mapRefund(result), nil
}

// RejectRefund rejects either stage 1 (Pending→Failed) or stage 2 (Processing→Failed).
// After rejection, schedules a short Restate timer to re-attempt escrow release for the
// associated order in case no other refunds block it.
func (b *OrderHandler) RejectRefund(
	ctx restate.Context,
	params RejectRefundParams,
) (ordermodel.Refund, error) {
	if params.RejectionNote == "" {
		return ordermodel.Refund{}, ordermodel.ErrRefundRejectionWithoutReason.Terminal()
	}

	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().RejectRefund(ctx, orderdb.RejectRefundParams{
			ID:            params.RefundID,
			RejectionNote: null.StringFrom(params.RejectionNote),
		})
	})
	if err != nil {
		return ordermodel.Refund{}, sharedmodel.WrapErr("reject refund", err)
	}

	// Re-fire escrow release (short delay) so payout can proceed if no other refunds block.
	item, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderItem, error) {
		return b.storage.Querier().GetItem(ctx, null.IntFrom(refund.OrderItemID))
	})
	if err == nil && item.OrderID.Valid {
		restate.ServiceSend(ctx, b.ServiceName(), "ReleaseEscrow").Send(
			ReleaseEscrowParams{OrderID: item.OrderID.UUID},
			restate.WithDelay(1*time.Minute),
		)
	}

	return mapRefund(refund), nil
}

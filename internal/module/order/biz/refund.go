package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// signalPayoutWorkflowOnRefundChanged fires a fire-and-forget signal to the
// PayoutWorkflow keyed on the refund's order ID, telling it to re-evaluate
// whether to release escrow or short-circuit into a refunded outcome.
func signalPayoutWorkflowOnRefundChanged(ctx restate.Context, orderID uuid.UUID) {
	restate.WorkflowSend(ctx, "PayoutWorkflow", orderID.String(), "OnRefundChanged").Send(struct{}{})
}

// CreateBuyerRefund creates a new 2-stage refund request for a paid order.
// A return transport is created automatically; the buyer supplies the return
// method and address. After the refund row is persisted, PayoutWorkflow is
// signalled so it can pause any pending escrow release.
func (b *OrderHandler) CreateBuyerRefund(
	ctx restate.Context,
	params CreateBuyerRefundParams,
) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	// Validate order ownership.
	if _, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderOrder, error) {
		o, e := b.storage.Querier().GetOrder(rctx, uuid.NullUUID{UUID: params.OrderID, Valid: true})
		if e != nil {
			return orderdb.OrderOrder{}, sharedmodel.WrapErr("get order", e)
		}
		if o.BuyerID != params.Account.ID {
			return orderdb.OrderOrder{}, ordermodel.ErrItemNotOwnedByBuyer.Terminal()
		}
		return o, nil
	}); err != nil {
		return zero, err
	}

	// At least one non-cancelled item must exist on the order.
	items, err := restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderItem, error) {
		return b.storage.Querier().ListItem(rctx, orderdb.ListItemParams{
			OrderID: []uuid.NullUUID{{UUID: params.OrderID, Valid: true}},
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list order items", err)
	}
	hasUncancelled := false
	for _, it := range items {
		if !it.DateCancelled.Valid {
			hasUncancelled = true
			break
		}
	}
	if !hasUncancelled {
		return zero, ordermodel.ErrItemAlreadyCancelled.Terminal()
	}

	// Guard against duplicate active refunds on the same order.
	active, err := restate.Run(ctx, func(rctx restate.RunContext) (bool, error) {
		return b.storage.Querier().HasActiveRefundForOrder(rctx, params.OrderID)
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("check active refund", err)
	}
	if active {
		return zero, ordermodel.ErrRefundAlreadyAccepted.Terminal()
	}

	// Create a placeholder return transport (logistics filled in when seller accepts stage 1).
	returnTransport, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderTransport, error) {
		return b.storage.Querier().CreateDefaultTransport(rctx, orderdb.CreateDefaultTransportParams{
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

	refund, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().CreateDefaultRefund(rctx, orderdb.CreateDefaultRefundParams{
			AccountID:   params.Account.ID,
			OrderID:     params.OrderID,
			TransportID: returnTransport.ID,
			Method:      params.Method,
			Reason:      params.Reason,
			Address:     addr,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create refund", err)
	}

	signalPayoutWorkflowOnRefundChanged(ctx, refund.OrderID)

	return mapRefund(refund), nil
}

// AcceptRefundStage1 is called by the seller to accept a Pending refund and move it to Processing.
// The seller provides shipping details for the buyer's return transport.
func (b *OrderHandler) AcceptRefundStage1(
	ctx restate.Context,
	params AcceptRefundStage1Params,
) (ordermodel.Refund, error) {
	refund, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().AcceptRefundStage1(rctx, orderdb.AcceptRefundStage1Params{
			ID:           params.RefundID,
			AcceptedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
		})
	})
	if err != nil {
		return ordermodel.Refund{}, sharedmodel.WrapErr("accept stage 1", err)
	}

	signalPayoutWorkflowOnRefundChanged(ctx, refund.OrderID)

	return mapRefund(refund), nil
}

// ApproveRefundStage2 is called by the seller after the returned items are received and inspected.
// It creates a refund tx (Success), credits the buyer's wallet, and cancels every non-cancelled
// item in the order. PayoutWorkflow is signalled afterwards so it self-cancels its pending payout
// session if any.
func (b *OrderHandler) ApproveRefundStage2(
	ctx restate.Context,
	params ApproveRefundStage2Params,
) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	refund, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().GetRefund(rctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get refund", err)
	}
	if refund.Status != orderdb.OrderStatusProcessing {
		return zero, ordermodel.ErrRefundStageSkipped.Terminal()
	}

	// Fetch order so we can authorise the seller and learn the buyer ID.
	order, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(rctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get order", err)
	}
	if order.SellerID != params.Account.ID {
		return zero, ordermodel.ErrItemNotOwnedBySeller.Terminal()
	}

	// All items in an order share a single checkout payment session and a
	// single buyer — pick the first non-cancelled item as our reference.
	items, err := restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderItem, error) {
		return b.storage.Querier().ListItem(rctx, orderdb.ListItemParams{
			OrderID: []uuid.NullUUID{{UUID: refund.OrderID, Valid: true}},
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list order items", err)
	}
	var anyItem orderdb.OrderItem
	var refundAmount int64
	for _, it := range items {
		if !it.DateCancelled.Valid {
			if anyItem.ID == 0 {
				anyItem = it
			}
			refundAmount += it.TotalAmount
		}
	}
	if anyItem.ID == 0 {
		return zero, sharedmodel.WrapErr("no non-cancelled items in order", ordermodel.ErrOrderItemNotFound)
	}

	// Infer buyer currency before the durable Run (cross-module call).
	buyerCurrency, err := b.inferCurrency(ctx, order.BuyerID)
	if err != nil {
		return zero, sharedmodel.WrapErr("infer buyer currency", err)
	}

	// Find the original payment tx in the buyer's checkout session — refund leg reverses it.
	sessionTxs, err := restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
		return b.storage.Querier().ListTransactionsBySession(rctx, anyItem.PaymentSessionID)
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list session txs", err)
	}
	var originalTxID null.Int
	for _, tx := range sessionTxs {
		if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 && !tx.ReversesID.Valid {
			originalTxID = null.IntFrom(tx.ID)
			break
		}
	}
	if !originalTxID.Valid {
		return zero, sharedmodel.WrapErr("no settled original tx in session", ordermodel.ErrOrderItemNotFound)
	}

	// All mutations in one durable Run: create refund leg, approve refund row, cancel every item.
	result, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderRefund, error) {
		refundTx, err := b.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
			SessionID:     anyItem.PaymentSessionID,
			Status:        orderdb.OrderStatusSuccess,
			Note:          fmt.Sprintf("refund approved for order %s", refund.OrderID),
			Error:         null.String{},
			PaymentOption: null.String{},
			WalletID:      uuid.NullUUID{},
			Data:          json.RawMessage("{}"),
			Amount:        -refundAmount,
			FromCurrency:  buyerCurrency,
			ToCurrency:    buyerCurrency,
			ExchangeRate:  mustNumericOne(),
			ReversesID:    originalTxID,
			DateSettled:   null.TimeFrom(time.Now()),
			DateExpired:   null.Time{},
		})
		if err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("create refund tx", err)
		}

		updated, err := b.storage.Querier().ApproveRefundStage2(rctx, orderdb.ApproveRefundStage2Params{
			ID:           refund.ID,
			ApprovedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			RefundTxID:   null.IntFrom(refundTx.ID),
		})
		if err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("approve stage 2", err)
		}

		for _, it := range items {
			if it.DateCancelled.Valid {
				continue
			}
			if _, err := b.storage.Querier().CancelItem(rctx, orderdb.CancelItemParams{
				ID:            it.ID,
				CancelledByID: uuid.NullUUID{UUID: refund.AccountID, Valid: true},
			}); err != nil {
				return orderdb.OrderRefund{}, sharedmodel.WrapErr("cancel item", err)
			}
		}

		return updated, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("approve stage 2", err)
	}

	// Credit buyer's wallet via the shared helper. CreditFromSession sums positive
	// Success txs in the session — the just-inserted negative refund leg is filtered
	// out, leaving the original settled amount.
	if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
		SessionID:  anyItem.PaymentSessionID,
		AccountID:  order.BuyerID,
		CreditType: "Refund",
		Reference:  fmt.Sprintf("refund:%s", refund.ID),
		Note:       "refund approved",
	}); err != nil {
		return zero, sharedmodel.WrapErr("wallet credit buyer", err)
	}

	signalPayoutWorkflowOnRefundChanged(ctx, refund.OrderID)

	return mapRefund(result), nil
}

// RejectRefund rejects either stage 1 (Pending->Failed) or stage 2 (Processing->Failed).
// PayoutWorkflow is signalled afterwards so it can resume its escrow timer if no other
// refund blocks payout.
func (b *OrderHandler) RejectRefund(
	ctx restate.Context,
	params RejectRefundParams,
) (ordermodel.Refund, error) {
	if params.RejectionNote == "" {
		return ordermodel.Refund{}, ordermodel.ErrRefundRejectionWithoutReason.Terminal()
	}

	refund, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().RejectRefund(rctx, orderdb.RejectRefundParams{
			ID:            params.RefundID,
			RejectionNote: null.StringFrom(params.RejectionNote),
		})
	})
	if err != nil {
		return ordermodel.Refund{}, sharedmodel.WrapErr("reject refund", err)
	}

	signalPayoutWorkflowOnRefundChanged(ctx, refund.OrderID)

	return mapRefund(refund), nil
}

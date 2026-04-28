package orderbiz

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/samber/lo"
)

// CreateRefundDispute opens a dispute on a Failed refund decision.
// Either buyer (refund.account_id) or seller (item.seller_id) may raise a dispute.
// A non-empty note is required.
func (b *OrderHandler) CreateRefundDispute(
	ctx restate.Context,
	params CreateRefundDisputeParams,
) (ordermodel.RefundDispute, error) {
	var zero ordermodel.RefundDispute
	var err error
	defer metrics.TrackHandler("order", "CreateRefundDispute", &err)()

	if err = validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create dispute", err)
	}
	if params.Note == "" {
		return zero, ordermodel.ErrDisputeNoteRequired.Terminal()
	}

	type disputeRunResult struct {
		BuyerID  uuid.UUID                  `json:"buyer_id"`
		SellerID uuid.UUID                  `json:"seller_id"`
		Dispute  orderdb.OrderRefundDispute `json:"dispute"`
	}

	result, err := restate.Run(ctx, func(ctx restate.RunContext) (disputeRunResult, error) {
		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("get refund", err)
		}

		// Dispute may only be raised against a Failed refund.
		if refund.Status != orderdb.OrderStatusFailed {
			return disputeRunResult{}, ordermodel.ErrInvalidDisputeState.Terminal()
		}

		// Fetch the order to determine seller identity (all items in an order share one seller).
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("get order", err)
		}

		// Permission: buyer (refund.AccountID) or seller (order.SellerID).
		if params.Account.ID != refund.AccountID && params.Account.ID != order.SellerID {
			return disputeRunResult{}, ordermodel.ErrUnauthorized.Terminal()
		}

		// Guard against duplicate active disputes on the same refund.
		activeCount, err := b.storage.Querier().CountRefundDispute(ctx, orderdb.CountRefundDisputeParams{
			RefundID: []uuid.UUID{params.RefundID},
			Status:   []orderdb.OrderStatus{orderdb.OrderStatusPending, orderdb.OrderStatusProcessing},
		})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("count active disputes", err)
		}
		if activeCount > 0 {
			return disputeRunResult{}, ordermodel.ErrDisputeAlreadyActive.Terminal()
		}

		dbDispute, err := b.storage.Querier().CreateDefaultRefundDispute(ctx, orderdb.CreateDefaultRefundDisputeParams{
			AccountID: params.Account.ID,
			RefundID:  params.RefundID,
			Reason:    params.Reason,
			Note:      params.Note,
		})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("db create dispute", err)
		}

		return disputeRunResult{
			BuyerID:  refund.AccountID,
			SellerID: order.SellerID,
			Dispute:  dbDispute,
		}, nil
	})
	if err != nil {
		return zero, err
	}

	dispute := mapRefundDispute(result.Dispute)

	// Notify the other party (fire-and-forget).
	var notifyAccountID uuid.UUID
	if params.Account.ID == result.BuyerID {
		notifyAccountID = result.SellerID
	} else {
		notifyAccountID = result.BuyerID
	}
	meta, _ := json.Marshal(map[string]string{
		"refund_id":  params.RefundID.String(),
		"dispute_id": dispute.ID.String(),
	})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: notifyAccountID,
		Type:      accountmodel.NotiDisputeOpened,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Refund dispute opened",
		Content:   "A dispute has been opened on a refund request.",
		Metadata:  meta,
	})

	metrics.DisputesCreatedTotal.Inc()

	return dispute, nil
}

// ListRefundDisputes lists disputes scoped by caller role.
// If RefundID is set, returns disputes for that refund (caller must be buyer or seller of the
// underlying item). Otherwise returns all disputes where account_id = caller.
func (b *OrderHandler) ListRefundDisputes(
	ctx restate.Context,
	params ListRefundDisputesParams,
) (sharedmodel.PaginateResult[ordermodel.RefundDispute], error) {
	var zero sharedmodel.PaginateResult[ordermodel.RefundDispute]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list disputes", err)
	}

	pagination := params.PaginationParams.Constrain()

	var refundIDFilter []uuid.UUID
	var accountIDFilter []uuid.UUID

	if params.RefundID.Valid {
		// Verify caller is buyer or seller before listing.
		type authCheck struct{}
		_, err := restate.Run(ctx, func(ctx restate.RunContext) (authCheck, error) {
			refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID.UUID, Valid: true})
			if err != nil {
				return authCheck{}, sharedmodel.WrapErr("get refund", err)
			}
			order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
			if err != nil {
				return authCheck{}, sharedmodel.WrapErr("get order", err)
			}
			if params.Account.ID != refund.AccountID && params.Account.ID != order.SellerID {
				return authCheck{}, ordermodel.ErrDisputeNotAuthorized.Terminal()
			}
			return authCheck{}, nil
		})
		if err != nil {
			return zero, err
		}
		refundIDFilter = []uuid.UUID{params.RefundID.UUID}
	} else {
		accountIDFilter = []uuid.UUID{params.Account.ID}
	}

	type listResult struct {
		Rows []orderdb.ListCountRefundDisputeRow `json:"rows"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (listResult, error) {
		rows, err := b.storage.Querier().ListCountRefundDispute(ctx, orderdb.ListCountRefundDisputeParams{
			RefundID:  refundIDFilter,
			AccountID: accountIDFilter,
			Offset:    pagination.Offset(),
			Limit:     pagination.Limit,
		})
		if err != nil {
			return listResult{}, err
		}
		return listResult{Rows: rows}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list disputes", err)
	}

	var total null.Int64
	if len(dbResult.Rows) > 0 {
		total.SetValid(dbResult.Rows[0].TotalCount)
	}

	return sharedmodel.PaginateResult[ordermodel.RefundDispute]{
		PageParams: pagination,
		Total:      total,
		Data: lo.Map(dbResult.Rows, func(r orderdb.ListCountRefundDisputeRow, _ int) ordermodel.RefundDispute {
			return mapRefundDispute(r.OrderRefundDispute)
		}),
	}, nil
}

// GetRefundDispute returns a single dispute by ID.
// The caller must be the buyer (refund.account_id) or seller (item.seller_id).
func (b *OrderHandler) GetRefundDispute(
	ctx restate.Context,
	params GetRefundDisputeParams,
) (ordermodel.RefundDispute, error) {
	var zero ordermodel.RefundDispute

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate get dispute", err)
	}

	type disputeResult struct {
		Dispute  orderdb.OrderRefundDispute `json:"dispute"`
		BuyerID  uuid.UUID                  `json:"buyer_id"`
		SellerID uuid.UUID                  `json:"seller_id"`
	}

	result, err := restate.Run(ctx, func(ctx restate.RunContext) (disputeResult, error) {
		dispute, err := b.storage.Querier().GetRefundDispute(ctx, uuid.NullUUID{UUID: params.DisputeID, Valid: true})
		if err != nil {
			return disputeResult{}, ordermodel.ErrDisputeNotFound.Terminal()
		}

		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: dispute.RefundID, Valid: true})
		if err != nil {
			return disputeResult{}, sharedmodel.WrapErr("get refund", err)
		}

		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
		if err != nil {
			return disputeResult{}, sharedmodel.WrapErr("get order", err)
		}

		return disputeResult{
			Dispute:  dispute,
			BuyerID:  refund.AccountID,
			SellerID: order.SellerID,
		}, nil
	})
	if err != nil {
		return zero, err
	}

	if params.Account.ID != result.BuyerID && params.Account.ID != result.SellerID {
		return zero, ordermodel.ErrDisputeNotAuthorized.Terminal()
	}

	return mapRefundDispute(result.Dispute), nil
}

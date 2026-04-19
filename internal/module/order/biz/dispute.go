package orderbiz

import (
	"encoding/json"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// dbToRefundDispute maps a DB OrderRefundDispute row to the model type.
func dbToRefundDispute(d orderdb.OrderRefundDispute) ordermodel.RefundDispute {
	return ordermodel.RefundDispute{
		ID:          d.ID,
		RefundID:    d.RefundID,
		IssuedByID:  d.IssuedByID,
		Reason:      d.Reason,
		Status:      d.Status,
		DateCreated: d.DateCreated,
		DateUpdated: d.DateUpdated,
	}
}

// CreateRefundDispute opens a dispute on a refund. Either the buyer or seller of the order may call this.
func (b *OrderHandler) CreateRefundDispute(
	ctx restate.Context,
	params CreateRefundDisputeParams,
) (_ ordermodel.RefundDispute, err error) {
	defer metrics.TrackHandler("order", "CreateRefundDispute", &err)()

	var zero ordermodel.RefundDispute

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create dispute", err)
	}

	// Fetch refund + order, check ownership, count active disputes, and insert — all in one
	// durable step to eliminate the race window between count and insert.
	type disputeRunResult struct {
		BuyerID  uuid.UUID                 `json:"buyer_id"`
		SellerID uuid.UUID                 `json:"seller_id"`
		Dispute  orderdb.OrderRefundDispute `json:"dispute"`
	}
	result, err := restate.Run(ctx, func(ctx restate.RunContext) (disputeRunResult, error) {
		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("get refund", err)
		}

		// Refund must not be resolved (Success) or Cancelled.
		if refund.Status == orderdb.OrderStatusSuccess || refund.Status == orderdb.OrderStatusCancelled {
			return disputeRunResult{}, ordermodel.ErrDisputeRefundResolved.Terminal()
		}

		// Check for an existing active dispute on this refund.
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

		// Fetch order to verify caller is buyer or seller.
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("get order", err)
		}

		if params.Account.ID != order.BuyerID && params.Account.ID != order.SellerID {
			return disputeRunResult{}, ordermodel.ErrDisputeNotAuthorized.Terminal()
		}

		// Insert dispute in the same transaction-like block.
		dbDispute, err := b.storage.Querier().CreateDefaultRefundDispute(ctx, orderdb.CreateDefaultRefundDisputeParams{
			RefundID:   params.RefundID,
			IssuedByID: params.Account.ID,
			Reason:     params.Reason,
		})
		if err != nil {
			return disputeRunResult{}, sharedmodel.WrapErr("db create dispute", err)
		}

		return disputeRunResult{
			BuyerID:  order.BuyerID,
			SellerID: order.SellerID,
			Dispute:  dbDispute,
		}, nil
	})
	if err != nil {
		return zero, err
	}

	dispute := dbToRefundDispute(result.Dispute)

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

// ListRefundDisputes lists disputes scoped by the caller's role.
// If RefundID is set, returns disputes for that refund (caller must be buyer or seller of the order).
// Otherwise returns all disputes issued by the caller.
func (b *OrderHandler) ListRefundDisputes(
	ctx restate.Context,
	params ListRefundDisputesParams,
) (sharedmodel.PaginateResult[ordermodel.RefundDispute], error) {
	var zero sharedmodel.PaginateResult[ordermodel.RefundDispute]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list disputes", err)
	}

	pagination := params.PaginationParams.Constrain()

	type listResult struct {
		Rows []orderdb.ListCountRefundDisputeRow `json:"rows"`
	}

	var refundIDFilter []uuid.UUID
	var issuedByIDFilter []uuid.UUID

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
			if params.Account.ID != order.BuyerID && params.Account.ID != order.SellerID {
				return authCheck{}, ordermodel.ErrDisputeNotAuthorized.Terminal()
			}
			return authCheck{}, nil
		})
		if err != nil {
			return zero, err
		}
		refundIDFilter = []uuid.UUID{params.RefundID.UUID}
	} else {
		issuedByIDFilter = []uuid.UUID{params.Account.ID}
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (listResult, error) {
		rows, err := b.storage.Querier().ListCountRefundDispute(ctx, orderdb.ListCountRefundDisputeParams{
			RefundID:   refundIDFilter,
			IssuedByID: issuedByIDFilter,
			Offset:     pagination.Offset(),
			Limit:      null.Int32From(pagination.Limit.Int32),
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
			return dbToRefundDispute(r.OrderRefundDispute)
		}),
	}, nil
}

// GetRefundDispute returns a single dispute by ID. The caller must be buyer or seller of the order.
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
			BuyerID:  order.BuyerID,
			SellerID: order.SellerID,
		}, nil
	})
	if err != nil {
		return zero, err
	}

	if params.Account.ID != result.BuyerID && params.Account.ID != result.SellerID {
		return zero, ordermodel.ErrDisputeNotAuthorized.Terminal()
	}

	return dbToRefundDispute(result.Dispute), nil
}

package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/saga"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ConfirmWorkflowHandler struct {
	base *OrderHandler
}

func NewConfirmWorkflowHandler(base *OrderHandler) *ConfirmWorkflowHandler {
	return &ConfirmWorkflowHandler{base: base}
}
func (h *ConfirmWorkflowHandler) ServiceName() string { return "ConfirmWorkflow" }

func (h *ConfirmWorkflowHandler) Run(
	ctx restate.WorkflowContext,
	input ConfirmWorkflowInput,
) (out ConfirmWorkflowOutput, err error) {
	defer metrics.TrackHandler("confirm_workflow", "Run", &err)()

	// Workflow ID set at submission time IS the confirm-fee session UUID —
	// every downstream insert / log / webhook routing key uses it.
	workflowID := restate.UUID(ctx)
	sessionID := workflowID

	// Step 1: Validate.
	if err = validator.Validate(input); err != nil {
		return out, sharedmodel.WrapErr("validate confirm", err)
	}

	saga := saga.New(ctx)
	defer func() {
		if restate.IsTerminalError(err) {
			saga.Compensate()
		}
	}()

	sellerID := input.Account.ID

	// Step 2: Lock seller pending so two concurrent confirms over an
	// overlapping ItemIDs slice can't double-spend wallet balance or skip
	// validation. The lock's lifetime is the workflow run.
	unlock := h.base.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", sellerID))
	defer unlock()

	// Step 3: Fetch and validate items inside a Run so list ordering and
	// missing-row checks are journaled (replay returns the exact same slice).
	orderItems, err := restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderItem, error) {
		items, e := h.base.storage.Querier().ListItem(rctx, orderdb.ListItemParams{
			ID: input.ItemIDs,
		})
		if e != nil {
			return nil, sharedmodel.WrapErr("db list items", e)
		}
		if len(items) != len(input.ItemIDs) {
			return nil, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		return items, nil
	})
	if err != nil {
		return out, sharedmodel.WrapErr("fetch items", err)
	}

	// Validate items and aggregate shared fields. Every item must be owned
	// by this seller, share buyer/address/transport, and not already be
	// final. paidTotal is the sum the buyer already paid for these items —
	// the eventual payout amount when escrow releases.
	var (
		buyerID                 uuid.UUID
		address                 string
		transportOption         string
		paidTotal               int64
		uniquePaymentSessionIDs = make(map[uuid.UUID]struct{})
	)
	// Validation outcome is deterministic over journaled orderItems, so any
	// failure must be terminal — otherwise Restate would retry the same
	// invariant violation forever. fmt.Errorf("%w") strips the .Terminal()
	// marker on ordermodel errors, so we re-wrap with restate.TerminalError.
	for i, item := range orderItems {
		if item.OrderID.Valid {
			return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyConfirmed))
		}
		if item.DateCancelled.Valid {
			return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyCancelled))
		}
		if item.SellerID != sellerID {
			return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemNotOwnedBySeller))
		}
		if i == 0 {
			buyerID = item.AccountID
			address = item.Address
			transportOption = item.TransportOption
		} else {
			if item.AccountID != buyerID {
				return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameBuyer))
			}
			if item.Address != address {
				return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameAddress))
			}
			if item.TransportOption != transportOption {
				return out, restate.TerminalError(fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsTransportMismatch))
			}
		}
		paidTotal += item.TotalAmount
		uniquePaymentSessionIDs[item.PaymentSessionID] = struct{}{}
	}

	// Step 4: Verify every unique buyer payment session already settled.
	// Confirming items the buyer never paid for would mint inventory for
	// free.
	for psID := range uniquePaymentSessionIDs {
		status, sErr := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderStatus, error) {
			session, e := h.base.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: psID, Valid: true})
			if e != nil {
				return "", sharedmodel.WrapErr("get payment session", e)
			}
			return session.Status, nil
		})
		if sErr != nil {
			return out, sharedmodel.WrapErr("check payment session status", sErr)
		}
		if status != orderdb.OrderStatusSuccess {
			return out, ordermodel.ErrPaymentNotSuccess.Terminal()
		}
	}

	// Step 5: Quote transport for the aggregate shipment. One quote covers
	// all confirmed items because they share the same transport_option and
	// destination address (asserted above).
	contactMap, err := h.base.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return out, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	transportClient, err := h.base.getTransportClient(transportOption)
	if err != nil {
		return out, err
	}

	transportItems := lo.Map(orderItems, func(item orderdb.OrderItem, _ int) transport.ItemMetadata {
		return transport.ItemMetadata{SkuID: item.SkuID, Quantity: item.Quantity}
	})
	quote, err := transportClient.Quote(ctx, transport.QuoteParams{
		Items:       transportItems,
		FromAddress: fromAddress,
		ToAddress:   address,
	})
	if err != nil {
		return out, sharedmodel.WrapErr("quote transport", err)
	}

	platformFee := int64(0) // TODO: plug config
	confirmFeeTotal := quote.Cost + platformFee

	// Confirm-fee txs are denominated in the seller's currency (the seller
	// is paying the platform). inferCurrency is cross-module → outside Run.
	sellerCurrency, err := h.base.InferCurrency(ctx, sellerID)
	if err != nil {
		return out, sharedmodel.WrapErr("infer seller currency", err)
	}

	// Step 6: Wallet / gateway split for confirmFeeTotal.
	var confirmFeeWallet, confirmFeeGateway int64
	if input.UseWallet && confirmFeeTotal > 0 {
		balance, balErr := h.base.GetWalletBalance(ctx, sellerID)
		if balErr != nil {
			return out, sharedmodel.WrapErr("get seller wallet balance", balErr)
		}
		if balance >= confirmFeeTotal {
			confirmFeeWallet = confirmFeeTotal
		} else {
			confirmFeeWallet = balance
		}
	}
	confirmFeeGateway = confirmFeeTotal - confirmFeeWallet

	if confirmFeeGateway > 0 && input.PaymentOption == "" {
		return out, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 7: Atomically create payment_session and child txs (Pending) in
	// one Run. Order / transport / item-link rows are deferred to the
	// post-paid Run below, so we don't have to compensate them on rollback.
	// Tx UUIDs are pre-allocated outside the closure: restate.UUID journals
	// them, so retries reuse the same value and INSERTs are idempotent on
	// PK conflict.
	walletTxID := restate.UUID(ctx)
	gatewayTxID := restate.UUID(ctx)

	txIDsToFail := make([]uuid.UUID, 0, 2)
	if confirmFeeWallet > 0 {
		txIDsToFail = append(txIDsToFail, walletTxID)
	}
	if confirmFeeGateway > 0 {
		txIDsToFail = append(txIDsToFail, gatewayTxID)
	}
	saga.Defer("mark_session_and_txs_failed", func(rctx restate.RunContext) error {
		return markSessionAndTxsFailed(rctx, h.base.storage.Querier(), sessionID, txIDsToFail, "confirm saga compensation")
	})

	if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
		if _, sErr := h.base.storage.Querier().CreateDefaultPaymentSession(rctx, orderdb.CreateDefaultPaymentSessionParams{
			ID:          sessionID,
			Kind:        SessionKindSellerConfirmationFee,
			Status:      orderdb.OrderStatusPending,
			FromID:      uuid.NullUUID{UUID: sellerID, Valid: true},
			ToID:        uuid.NullUUID{},
			Note:        "seller confirmation fee",
			Currency:    sellerCurrency,
			TotalAmount: confirmFeeTotal,
			Data:        json.RawMessage("{}"),
			DatePaid:    null.Time{},
			DateExpired: time.Now().Add(paymentExpiry),
		}); sErr != nil {
			return sharedmodel.WrapErr("db create confirm session", sErr)
		}

		if confirmFeeWallet > 0 {
			if _, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				ID:           walletTxID,
				SessionID:    sessionID,
				Status:       orderdb.OrderStatusPending,
				Note:         "confirm fee wallet payment",
				Data:         json.RawMessage("{}"),
				Amount:       confirmFeeWallet,
				FromCurrency: sellerCurrency,
				ToCurrency:   sellerCurrency,
				ExchangeRate: mustNumericOne(),
			}); txErr != nil {
				return sharedmodel.WrapErr("db create confirm_fee wallet tx", txErr)
			}
		}

		if confirmFeeGateway > 0 {
			if _, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				ID:            gatewayTxID,
				SessionID:     sessionID,
				Status:        orderdb.OrderStatusPending,
				Note:          "confirm fee gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(input.PaymentOption),
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeGateway,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				ReversesID:    uuid.NullUUID{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			}); txErr != nil {
				return sharedmodel.WrapErr("db create confirm_fee gateway tx", txErr)
			}
		}

		return nil
	}); err != nil {
		return out, sharedmodel.WrapErr("create confirm fee session and txs", err)
	}

	// Step 8: Wallet debit + mark wallet tx success.
	if confirmFeeWallet > 0 {
		if _, dErr := restate.Run(ctx, func(rctx restate.RunContext) (WalletDebitResult, error) {
			return h.base.walletDebit(rctx, WalletDebitParams{
				AccountID: sellerID,
				Amount:    confirmFeeWallet,
				Reference: fmt.Sprintf("tx:%s", walletTxID),
				Note:      "confirm fee wallet payment",
			})
		}); dErr != nil {
			return out, sharedmodel.WrapErr("seller wallet debit", dErr)
		}
		// Arm credit compensator AFTER debit confirmed. walletDebit's DB tx
		// rolls back on any error → terminal failure means no debit, so
		// arming earlier would over-credit on saga fire. Compensator goes
		// straight to CreditInternalWallet to avoid the RunContext/Context
		// double-wrap (saga.Compensate already runs inside RunVoid).
		saga.Defer("credit_wallet", func(rctx restate.RunContext) error {
			_, e := h.base.storage.Querier().CreditInternalWallet(rctx, orderdb.CreditInternalWalletParams{
				ID:     sellerID,
				Amount: confirmFeeWallet,
			})
			return e
		})
		if mErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			_, e := h.base.storage.Querier().MarkTransactionSuccess(rctx, orderdb.MarkTransactionSuccessParams{
				ID:          walletTxID,
				DateSettled: time.Now(),
			})
			return e
		}); mErr != nil {
			return out, sharedmodel.WrapErr("mark confirm-fee wallet tx success", mErr)
		}
	}

	// Step 9: Initiate gateway payment + publish payment_url so HTTP attach
	// can return synchronously. The promise must be resolved on every path
	// (including wallet-only) so attached callers don't hang.
	url, gErr := h.base.InitGatewayPayment(ctx, InitGatewayPaymentParams{
		TxID:          gatewayTxID,
		Amount:        confirmFeeGateway,
		PaymentOption: input.PaymentOption,
		Description:   fmt.Sprintf("Confirm fee session %s", workflowID),
	})
	if gErr != nil {
		return out, gErr
	}
	if pErr := restate.Promise[string](ctx, "payment_url").Resolve(url); pErr != nil {
		return out, sharedmodel.WrapErr("resolve payment url promise", pErr)
	}

	// Step 10: Wait for payment outcome, seller cancel, or expiry. Cancel
	// is a separate promise (matching checkout) because payment.Status has
	// no `cancelled` value.
	eventPromise := restate.Promise[payment.Notification](ctx, "payment_event")
	cancelPromise := restate.Promise[struct{}](ctx, "user_cancel")
	expiryFut := restate.After(ctx, paymentExpiry)

	if confirmFeeGateway == 0 {
		_ = eventPromise.Resolve(payment.Notification{
			RefID:  sessionID.String(),
			Status: payment.StatusSuccess,
		})
	}

	done, _ := restate.WaitFirst(ctx, eventPromise, cancelPromise, expiryFut)
	switch done {
	case eventPromise:
		ev, evErr := eventPromise.Result()
		if evErr != nil {
			return out, sharedmodel.WrapErr("read payment event", evErr)
		}
		switch ev.Status {
		case payment.StatusSuccess:
			// fall through to success tail.
		case payment.StatusFailed, payment.StatusExpired:
			return out, ordermodel.ErrPaymentFailed.Terminal()
		default:
			return out, sharedmodel.WrapErr("unknown payment event status", ordermodel.ErrPaymentFailed)
		}
	case cancelPromise:
		// Inline-compensate before returning non-terminal: rolls back the
		// confirm-fee Pending session/txs and refunds the seller's wallet
		// debit. The defer's IsTerminalError check ignores nil err, so
		// there's no double-compensation on top of this explicit call.
		saga.Compensate()
		return ConfirmWorkflowOutput{Status: "cancelled", ConfirmSessionID: sessionID}, nil
	case expiryFut:
		saga.Compensate()
		return ConfirmWorkflowOutput{Status: "expired", ConfirmSessionID: sessionID}, nil
	}

	// Step 11: Paid path — clear saga and atomically create transport,
	// order, and item links in one Run. Failure here is a programmer bug
	// (the seller already paid us); we let it bubble as terminal.
	saga.Clear()

	type postPayRunResult struct {
		Order     orderdb.OrderOrder     `json:"order"`
		Transport orderdb.OrderTransport `json:"transport"`
	}
	postPay, err := restate.Run(ctx, func(rctx restate.RunContext) (postPayRunResult, error) {
		var res postPayRunResult

		quoteData, _ := json.Marshal(map[string]int64{"quote": quote.Cost})
		trRow, tErr := h.base.storage.Querier().CreateDefaultTransport(rctx, orderdb.CreateDefaultTransportParams{
			Option: transportOption,
			Data:   json.RawMessage(quoteData),
		})
		if tErr != nil {
			return res, sharedmodel.WrapErr("db create transport", tErr)
		}
		res.Transport = trRow

		order, oErr := h.base.storage.Querier().CreateDefaultOrder(rctx, orderdb.CreateDefaultOrderParams{
			BuyerID:          buyerID,
			SellerID:         sellerID,
			TransportID:      trRow.ID,
			Address:          address,
			ConfirmedByID:    input.Account.ID,
			ConfirmSessionID: sessionID,
			Note:             null.NewString(input.Note, input.Note != ""),
		})
		if oErr != nil {
			return res, sharedmodel.WrapErr("db create order", oErr)
		}
		res.Order = order

		if lErr := h.base.storage.Querier().SetItemsOrderID(rctx, orderdb.SetItemsOrderIDParams{
			OrderID: uuid.NullUUID{UUID: order.ID, Valid: true},
			ItemIds: input.ItemIDs,
		}); lErr != nil {
			return res, sharedmodel.WrapErr("db set items order id", lErr)
		}

		return res, nil
	})
	if err != nil {
		return out, err
	}

	metrics.OrdersCreatedTotal.Inc()

	// Step 12: Notify buyer (fire-and-forget).
	itemNames := lo.Map(orderItems, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
	summary := ordermodel.SummarizeNames(itemNames)
	notiMeta, _ := json.Marshal(map[string]string{"order_id": postPay.Order.ID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: buyerID,
		Type:      accountmodel.NotiItemsConfirmed,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Items confirmed",
		Content:   fmt.Sprintf("%s has been confirmed by the seller.", summary),
		Metadata:  notiMeta,
	})

	// Step 13: Hand off to PayoutWorkflow. Its workflow ID is the order ID,
	// so the eventual escrow release / refund signals can target it without
	// a side table.
	restate.WorkflowSend(ctx, "PayoutWorkflow", postPay.Order.ID.String(), "Run").Send(PayoutInput{
		OrderID:   postPay.Order.ID,
		SellerID:  sellerID,
		PaidTotal: paidTotal,
		Currency:  sellerCurrency,
	})

	return ConfirmWorkflowOutput{
		Status:           "confirmed",
		OrderID:          postPay.Order.ID,
		ConfirmSessionID: sessionID,
	}, nil
}

// WaitPaymentURL blocks the caller until Run() resolves the `payment_url`
// promise. Used by the HTTP transport to turn the async workflow submission
// into a sync response carrying the gateway redirect (or an empty string for
// wallet-only confirms).
func (h *ConfirmWorkflowHandler) WaitPaymentURL(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) (string, error) {
	return restate.Promise[string](ctx, "payment_url").Result()
}

// PaymentNotification is called by the payment provider
func (h *ConfirmWorkflowHandler) PaymentNotification(
	ctx restate.WorkflowSharedContext,
	noti payment.Notification,
) error {
	return restate.Promise[payment.Notification](ctx, "payment_event").Resolve(noti)
}

// CancelConfirm lets the seller abort an in-flight confirm.
func (h *ConfirmWorkflowHandler) CancelConfirm(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) error {
	return restate.Promise[struct{}](ctx, "user_cancel").Resolve(struct{}{})
}

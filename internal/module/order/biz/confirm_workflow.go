package orderbiz

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// ConfirmWorkflowHandler is the Restate Workflow that drives a seller's
// confirm-pending action from "submitted" through
// "confirmed | expired | cancelled". It is a port of
// OrderHandler.ConfirmSellerPending: side effects guarded by saga compensators
// (see saga.go) and the confirm-fee payment outcome awaited via a durable
// promise + expiry timer.
type ConfirmWorkflowHandler struct {
	*OrderHandler
}

// NewConfirmWorkflowHandler wraps an OrderHandler so the workflow can reuse
// every helper (storage, locker, account/catalog/inventory/common clients,
// payment/transport maps, MarkTxSuccess, CreditFromSession, etc.).
func NewConfirmWorkflowHandler(base *OrderHandler) *ConfirmWorkflowHandler {
	return &ConfirmWorkflowHandler{OrderHandler: base}
}

// ServiceName overrides the embedded OrderHandler's name so restate.Reflect
// registers this struct under "ConfirmWorkflow".
func (h *ConfirmWorkflowHandler) ServiceName() string { return "ConfirmWorkflow" }

// Run is the workflow body. The HTTP transport allocates a UUID, uses its
// string form as the workflow ID when calling restate.Workflow(...), and the
// workflow recovers it via ctx.Key() — the confirm-fee session row is then
// INSERTed with that exact UUID. Workflow ID == confirm session ID == webhook
// RefID, so attached callers can read `payment_url` before Run reaches its
// publish step and webhooks can route directly without a DB lookup.
//
// Saga semantics: every committed side effect (confirm-fee session/tx rows,
// wallet debit) is paired with a Defer() compensator BEFORE the action runs.
// On the success path we Clear(); on terminal failure / cancel / expiry the
// LIFO compensators execute via restate.RunVoid (idempotent retry). Order /
// transport / item-link rows are only created on the paid path, so they
// never need compensators.
func (h *ConfirmWorkflowHandler) Run(
	ctx restate.WorkflowContext,
	input ConfirmWorkflowInput,
) (out ConfirmWorkflowOutput, err error) {
	defer metrics.TrackHandler("confirm_workflow", "Run", &err)()

	// Workflow ID set at submission time IS the confirm-fee session UUID.
	// Parse once so every downstream insert / log / webhook routing key uses
	// the same identifier. A bad workflow ID is a programmer error from the
	// caller — terminal, no retry.
	workflowID := restate.Key(ctx)
	sessionID, err := uuid.Parse(workflowID)
	if err != nil {
		return out, restate.TerminalError(sharedmodel.WrapErr("invalid workflow id (expected uuid)", err))
	}

	// Step 1: Validate.
	if err = validator.Validate(input); err != nil {
		return out, sharedmodel.WrapErr("validate confirm", err)
	}

	sellerID := input.Account.ID

	// Step 2: Lock seller pending so two concurrent confirms over an
	// overlapping ItemIDs slice can't double-spend wallet balance or skip
	// validation. The lock's lifetime is the workflow run.
	unlock := h.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", sellerID))
	defer unlock()

	// Step 3: Fetch and validate items inside a Run so list ordering and
	// missing-row checks are journaled (replay returns the exact same slice).
	orderItems, err := restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderItem, error) {
		items, e := h.storage.Querier().ListItem(rctx, orderdb.ListItemParams{
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
	for i, item := range orderItems {
		if item.OrderID.Valid {
			return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyConfirmed)
		}
		if item.DateCancelled.Valid {
			return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyCancelled)
		}
		if item.SellerID != sellerID {
			return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemNotOwnedBySeller)
		}
		if i == 0 {
			buyerID = item.AccountID
			address = item.Address
			transportOption = item.TransportOption
		} else {
			if item.AccountID != buyerID {
				return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameBuyer)
			}
			if item.Address != address {
				return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameAddress)
			}
			if item.TransportOption != transportOption {
				return out, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsTransportMismatch)
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
			session, e := h.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: psID, Valid: true})
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
	contactMap, err := h.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return out, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	transportClient, err := h.getTransportClient(transportOption)
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
	sellerCurrency, err := h.inferCurrency(ctx, sellerID)
	if err != nil {
		return out, sharedmodel.WrapErr("infer seller currency", err)
	}

	// Step 6: Wallet / gateway split for confirmFeeTotal.
	var confirmFeeWallet, confirmFeeGateway int64
	if input.UseWallet && confirmFeeTotal > 0 {
		balance, balErr := h.account.GetWalletBalance(ctx, sellerID)
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

	// Saga setup. Compensators are registered BEFORE side effects; on err /
	// cancel / expire we run them LIFO. CreditFromSession is invoked from
	// this top-level defer (cross-module → can't run inside saga RunVoid).
	saga := NewSaga(ctx)
	var (
		cancelled                     bool
		expired                       bool
		confirmSessionForCompensation uuid.UUID
	)
	defer func() {
		if err != nil || cancelled || expired {
			saga.Compensate()
			if confirmSessionForCompensation != uuid.Nil {
				if _, ce := h.CreditFromSession(ctx, CreditFromSessionParams{
					SessionID:  confirmSessionForCompensation,
					AccountID:  sellerID,
					CreditType: "Refund",
					Note:       "confirm saga compensation",
				}); ce != nil {
					slog.Error("confirm compensation: credit from session", slog.Any("error", ce))
				}
			}
			if err != nil && !restate.IsTerminalError(err) {
				err = restate.TerminalError(err)
			}
		}
	}()

	// Step 7: Atomically create the confirm-fee session and its child txs.
	// Order / transport / item-link rows are deferred to the post-paid run
	// below, so we don't have to compensate them on rollback.
	type confirmRunResult struct {
		Session         orderdb.OrderPaymentSession `json:"session"`
		WalletTx        *orderdb.OrderTransaction   `json:"wallet_tx,omitempty"`
		GatewayTx       *orderdb.OrderTransaction   `json:"gateway_tx,omitempty"`
		ConfirmFeeTxIDs []int64                     `json:"confirm_fee_tx_ids"`
		BlockerTxID     int64                       `json:"blocker_tx_id"`
	}

	// Compensator declared BEFORE the Run so a panic mid-Run still triggers
	// cleanup. MarkPaymentSessionFailed is idempotent on already-final rows.
	saga.Defer("mark_confirm_session_failed", func(rctx restate.RunContext) error {
		if confirmSessionForCompensation == uuid.Nil {
			return nil
		}
		if _, e := h.storage.Querier().MarkPaymentSessionFailed(rctx, confirmSessionForCompensation); e != nil {
			return e
		}
		return nil
	})

	created, err := restate.Run(ctx, func(rctx restate.RunContext) (confirmRunResult, error) {
		var res confirmRunResult

		session, sErr := h.storage.Querier().CreateDefaultPaymentSession(rctx, orderdb.CreateDefaultPaymentSessionParams{
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
		})
		if sErr != nil {
			return res, sharedmodel.WrapErr("db create confirm session", sErr)
		}
		res.Session = session

		if confirmFeeWallet > 0 {
			tx, txErr := h.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     session.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "confirm fee wallet payment",
				Error:         null.String{},
				PaymentOption: null.String{},
				WalletID:      uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeWallet,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.Time{},
			})
			if txErr != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee wallet tx", txErr)
			}
			res.WalletTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		if confirmFeeGateway > 0 {
			tx, txErr := h.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     session.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "confirm fee gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(input.PaymentOption),
				WalletID:      toNullUUID(input.WalletID),
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeGateway,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			})
			if txErr != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee gateway tx", txErr)
			}
			res.GatewayTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID // gateway wins as blocker
		}

		return res, nil
	})
	if err != nil {
		return out, sharedmodel.WrapErr("create confirm fee session and txs", err)
	}
	confirmSessionForCompensation = created.Session.ID

	// Step 8: Wallet debit + mark wallet tx success (cross-module → outside
	// Run). The credit_wallet compensator must be deferred BEFORE the debit.
	if confirmFeeWallet > 0 && created.WalletTx != nil {
		walletTxID := created.WalletTx.ID
		amount := confirmFeeWallet
		saga.Defer("credit_wallet", func(rctx restate.RunContext) error {
			return h.account.WalletCredit(rctx, accountbiz.WalletCreditParams{
				AccountID: sellerID,
				Amount:    amount,
				Type:      "Refund",
				Reference: fmt.Sprintf("compensate-tx:%d", walletTxID),
				Note:      "confirm saga compensation",
			})
		})
		if _, dErr := h.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: sellerID,
			Amount:    amount,
			Reference: fmt.Sprintf("tx:%d", walletTxID),
			Note:      "confirm fee wallet payment",
		}); dErr != nil {
			return out, sharedmodel.WrapErr("seller wallet debit", dErr)
		}
		if mErr := h.MarkTxSuccess(ctx, MarkTxSuccessParams{TxID: walletTxID}); mErr != nil {
			return out, sharedmodel.WrapErr("mark confirm-fee wallet tx success", mErr)
		}
	}

	// Step 9: Initiate gateway payment + publish payment_url so HTTP attach
	// can return synchronously. The promise must be resolved on every path
	// (including wallet-only) so attached callers don't hang.
	var url string
	if confirmFeeGateway > 0 {
		paymentClient, pcErr := h.getPaymentClient(input.PaymentOption)
		if pcErr != nil {
			return out, sharedmodel.WrapErr("get payment client", pcErr)
		}

		blockerTxID := created.BlockerTxID
		gatewayAmt := confirmFeeGateway
		// RefID == workflow ID (== session UUID string) so the webhook can
		// route back to this exact workflow without a DB lookup.
		paymentRef := workflowID
		res, rErr := restate.Run(ctx, func(rctx restate.RunContext) (string, error) {
			r, e := paymentClient.Create(rctx, payment.CreateParams{
				RefID:       paymentRef,
				Amount:      gatewayAmt,
				Description: fmt.Sprintf("Confirm fee session %s", paymentRef),
			})
			if e != nil {
				return "", e
			}
			return r.RedirectURL, nil
		})
		if rErr != nil {
			return out, sharedmodel.WrapErr("create gateway payment for confirm fee", rErr)
		}
		url = res

		if url != "" {
			if pErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				data, _ := json.Marshal(map[string]string{"gateway_url": url})
				return h.storage.Querier().SetTransactionData(rctx, orderdb.SetTransactionDataParams{
					ID:   blockerTxID,
					Data: data,
				})
			}); pErr != nil {
				return out, sharedmodel.WrapErr("persist gateway url on tx", pErr)
			}
		}
	}
	if pErr := restate.Promise[string](ctx, "payment_url").Resolve(url); pErr != nil {
		return out, sharedmodel.WrapErr("resolve payment url promise", pErr)
	}

	// Step 10: Wait for payment outcome or expiry. Wallet-only confirms
	// short-circuit: the wallet leg is already Success, so we resolve the
	// event promise inline as "paid" before WaitFirst.
	eventPromise := restate.Promise[PaymentEvent](ctx, "payment_event")
	expiryFut := restate.After(ctx, paymentExpiry)

	if confirmFeeGateway == 0 {
		_ = eventPromise.Resolve(PaymentEvent{Kind: "paid", TxID: created.BlockerTxID})
	}

	done, _ := restate.WaitFirst(ctx, eventPromise, expiryFut)
	switch done {
	case eventPromise:
		ev, evErr := eventPromise.Result()
		if evErr != nil {
			return out, sharedmodel.WrapErr("read payment event", evErr)
		}
		switch ev.Kind {
		case "paid":
			// fall through to success tail.
		case "cancelled":
			cancelled = true
			return ConfirmWorkflowOutput{Status: "cancelled", ConfirmSessionID: sessionID}, nil
		case "failed":
			return out, ordermodel.ErrPaymentFailed.Terminal()
		default:
			return out, sharedmodel.WrapErr("unknown payment event kind", ordermodel.ErrPaymentFailed)
		}
	case expiryFut:
		expired = true
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
		trRow, tErr := h.storage.Querier().CreateDefaultTransport(rctx, orderdb.CreateDefaultTransportParams{
			Option: transportOption,
			Data:   json.RawMessage(quoteData),
		})
		if tErr != nil {
			return res, sharedmodel.WrapErr("db create transport", tErr)
		}
		res.Transport = trRow

		order, oErr := h.storage.Querier().CreateDefaultOrder(rctx, orderdb.CreateDefaultOrderParams{
			BuyerID:          buyerID,
			SellerID:         sellerID,
			TransportID:      trRow.ID,
			Address:          address,
			ConfirmedByID:    input.Account.ID,
			ConfirmSessionID: created.Session.ID,
			Note:             null.NewString(input.Note, input.Note != ""),
		})
		if oErr != nil {
			return res, sharedmodel.WrapErr("db create order", oErr)
		}
		res.Order = order

		if lErr := h.storage.Querier().SetItemsOrderID(rctx, orderdb.SetItemsOrderIDParams{
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

// ResolvePayment is the webhook entry point for the Restate side: callers
// (the payment webhook handler / wallet success path) invoke it to push a
// terminal PaymentEvent into the workflow's event promise, which Run() races
// against the expiry timer.
func (h *ConfirmWorkflowHandler) ResolvePayment(
	ctx restate.WorkflowSharedContext,
	ev PaymentEvent,
) error {
	return restate.Promise[PaymentEvent](ctx, "payment_event").Resolve(ev)
}

// CancelConfirm lets the seller abort an in-flight confirm. It resolves the
// event promise with kind="cancelled", which Run() interprets as a terminal
// rollback (compensators run via the deferred saga).
func (h *ConfirmWorkflowHandler) CancelConfirm(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) error {
	return restate.Promise[PaymentEvent](ctx, "payment_event").Resolve(PaymentEvent{Kind: "cancelled"})
}

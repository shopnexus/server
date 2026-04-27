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
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// ConfirmSellerPending groups selected pending items into an order with transport and fee transactions.
func (b *OrderHandler) ConfirmSellerPending(
	ctx restate.Context,
	params ConfirmSellerPendingParams,
) (_ ConfirmSellerPendingResult, err error) {
	defer metrics.TrackHandler("order", "ConfirmSellerPending", &err)()

	var zero ConfirmSellerPendingResult

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate confirm items", err)
	}

	sellerID := params.Account.ID

	// Step 1: Acquire seller lock.
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", sellerID))
	defer unlock()

	// Step 2: Fetch and validate items.
	orderItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		items, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: params.ItemIDs,
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("db list items", err)
		}
		if len(items) != len(params.ItemIDs) {
			return nil, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		return items, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch items", err)
	}

	// Validate items and aggregate shared fields.
	var buyerID uuid.UUID
	var address string
	var transportOption string
	var paidTotal int64
	uniquePaymentSessionIDs := make(map[int64]struct{})

	for i, item := range orderItems {
		if item.OrderID.Valid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyConfirmed)
		}
		if item.DateCancelled.Valid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyCancelled)
		}
		if item.SellerID != sellerID {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemNotOwnedBySeller)
		}
		if i == 0 {
			buyerID = item.AccountID
			address = item.Address
			transportOption = item.TransportOption
		} else {
			if item.AccountID != buyerID {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameBuyer)
			}
			if item.Address != address {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameAddress)
			}
			if item.TransportOption != transportOption {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsTransportMismatch)
			}
		}
		paidTotal += item.TotalAmount
		uniquePaymentSessionIDs[item.PaymentSessionID] = struct{}{}
	}

	// Step 3: Verify every unique payment session is Success.
	for sessionID := range uniquePaymentSessionIDs {
		status, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderStatus, error) {
			session, err := b.storage.Querier().GetPaymentSession(ctx, null.IntFrom(sessionID))
			if err != nil {
				return "", sharedmodel.WrapErr("get payment session", err)
			}
			return session.Status, nil
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("check payment session status", err)
		}
		if status != orderdb.OrderStatusSuccess {
			return zero, ordermodel.ErrPaymentNotSuccess.Terminal()
		}
	}

	// Step 4: Get seller contact address for transport from_address.
	contactMap, err := b.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return zero, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	// Step 5: Get transport client and quote cost.
	transportClient, err := b.getTransportClient(transportOption)
	if err != nil {
		return zero, err
	}

	transportItems := lo.Map(orderItems, func(item orderdb.OrderItem, _ int) transport.ItemMetadata {
		return transport.ItemMetadata{
			SkuID:    item.SkuID,
			Quantity: item.Quantity,
		}
	})

	quote, err := transportClient.Quote(ctx, transport.QuoteParams{
		Items:       transportItems,
		FromAddress: fromAddress,
		ToAddress:   address,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("quote transport", err)
	}

	platformFee := int64(0) // TODO: plug config
	confirmFeeTotal := quote.Cost + platformFee

	// Infer seller currency for all transactions in this flow (outside Run — cross-module).
	sellerCurrency, err := b.inferCurrency(ctx, sellerID)
	if err != nil {
		return zero, sharedmodel.WrapErr("infer seller currency", err)
	}

	// Step 6: Wallet / gateway split for confirmFeeTotal.
	var confirmFeeWallet, confirmFeeGateway int64
	if params.UseWallet && confirmFeeTotal > 0 {
		balance, err := b.account.GetWalletBalance(ctx, sellerID)
		if err != nil {
			return zero, sharedmodel.WrapErr("get seller wallet balance", err)
		}
		if balance >= confirmFeeTotal {
			confirmFeeWallet = confirmFeeTotal
		} else {
			confirmFeeWallet = balance
		}
	}
	confirmFeeGateway = confirmFeeTotal - confirmFeeWallet

	if confirmFeeGateway > 0 && params.PaymentOption == "" {
		return zero, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 7: Create transport, confirm_fee session+txs, payout session+tx, order, link items — all in one Run.
	type confirmRunResult struct {
		Transport       orderdb.OrderTransport      `json:"transport"`
		ConfirmSession  orderdb.OrderPaymentSession `json:"confirm_session"`
		PayoutSession   orderdb.OrderPaymentSession `json:"payout_session"`
		WalletTx        *orderdb.OrderTransaction   `json:"wallet_tx,omitempty"`
		GatewayTx       *orderdb.OrderTransaction   `json:"gateway_tx,omitempty"`
		PayoutTx        orderdb.OrderTransaction    `json:"payout_tx"`
		Order           orderdb.OrderOrder          `json:"order"`
		ConfirmFeeTxIDs []int64                     `json:"confirm_fee_tx_ids"`
		BlockerTxID     int64                       `json:"blocker_tx_id"`
	}

	runRes, err := restate.Run(ctx, func(ctx restate.RunContext) (confirmRunResult, error) {
		var res confirmRunResult

		// Create transport record.
		quoteData, _ := json.Marshal(map[string]int64{"quote": quote.Cost})
		trRow, err := b.storage.Querier().CreateDefaultTransport(ctx, orderdb.CreateDefaultTransportParams{
			Option: transportOption,
			Data:   json.RawMessage(quoteData),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create transport", err)
		}
		res.Transport = trRow

		// Confirm-fee session: seller pays platform for transport. Always Pending at
		// create-time; auto-promoted to Success by MarkTxSuccess when every child tx
		// settles (wallet leg after WalletDebit ack; gateway leg after webhook).
		confirmFeeTotal := confirmFeeWallet + confirmFeeGateway
		confirmSession, err := b.storage.Querier().CreateDefaultPaymentSession(ctx, orderdb.CreateDefaultPaymentSessionParams{
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
		if err != nil {
			return res, sharedmodel.WrapErr("db create confirm session", err)
		}
		res.ConfirmSession = confirmSession

		// Confirm-fee wallet tx (if any) — Pending until WalletDebit cross-module ack.
		if confirmFeeWallet > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     confirmSession.ID,
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
			if err != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee wallet tx", err)
			}
			res.WalletTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		// Confirm-fee gateway tx (if any).
		if confirmFeeGateway > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     confirmSession.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "confirm fee gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(params.PaymentOption),
				WalletID:      toNullUUID(params.WalletID),
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeGateway,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			})
			if err != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee gateway tx", err)
			}
			res.GatewayTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID // gateway wins as blocker
		}

		// Payout session: system → seller, Pending until escrow releases.
		// date_expired set far-future since escrow window is days, not minutes.
		payoutSession, err := b.storage.Querier().CreateDefaultPaymentSession(ctx, orderdb.CreateDefaultPaymentSessionParams{
			Kind:        SessionKindSellerPayout,
			Status:      orderdb.OrderStatusPending,
			FromID:      uuid.NullUUID{},
			ToID:        uuid.NullUUID{UUID: sellerID, Valid: true},
			Note:        "seller payout (escrow)",
			Currency:    sellerCurrency,
			TotalAmount: paidTotal,
			Data:        json.RawMessage("{}"),
			DatePaid:    null.Time{},
			DateExpired: time.Now().Add(365 * 24 * time.Hour),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create payout session", err)
		}
		res.PayoutSession = payoutSession

		// Payout tx in payout session — single rail, Pending until escrow release.
		payoutTx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
			SessionID:     payoutSession.ID,
			Status:        orderdb.OrderStatusPending,
			Note:          "seller payout (escrow)",
			Error:         null.String{},
			PaymentOption: null.String{},
			WalletID:      uuid.NullUUID{},
			Data:          json.RawMessage("{}"),
			Amount:        paidTotal,
			FromCurrency:  sellerCurrency,
			ToCurrency:    sellerCurrency,
			ExchangeRate:  mustNumericOne(),
			ReversesID:    null.Int{},
			DateSettled:   null.Time{},
			DateExpired:   null.Time{},
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create payout tx", err)
		}
		res.PayoutTx = payoutTx

		// Create order (confirm_session_id = the seller's confirmation-fee session).
		order, err := b.storage.Querier().CreateDefaultOrder(ctx, orderdb.CreateDefaultOrderParams{
			BuyerID:          buyerID,
			SellerID:         sellerID,
			TransportID:      trRow.ID,
			Address:          address,
			ConfirmedByID:    params.Account.ID,
			ConfirmSessionID: confirmSession.ID,
			Note:             null.NewString(params.Note, params.Note != ""),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create order", err)
		}
		res.Order = order

		// Link items to order.
		if err = b.storage.Querier().SetItemsOrderID(ctx, orderdb.SetItemsOrderIDParams{
			OrderID: uuid.NullUUID{UUID: order.ID, Valid: true},
			ItemIds: params.ItemIDs,
		}); err != nil {
			return res, sharedmodel.WrapErr("db set items order id", err)
		}

		return res, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create order", err)
	}

	metrics.OrdersCreatedTotal.Inc()

	// Step 8: Debit seller wallet for confirm fee (outside Run — cross-module).
	// On debit success, mark the wallet tx Success — auto-promotes the confirm
	// session if there's no gateway leg.
	if confirmFeeWallet > 0 && runRes.WalletTx != nil {
		if _, err = b.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: sellerID,
			Amount:    confirmFeeWallet,
			Reference: fmt.Sprintf("tx:%d", runRes.WalletTx.ID),
			Note:      "confirm fee wallet payment",
		}); err != nil {
			return zero, sharedmodel.WrapErr("seller wallet debit", err)
		}
		if err := b.MarkTxSuccess(ctx, MarkTxSuccessParams{TxID: runRes.WalletTx.ID}); err != nil {
			return zero, sharedmodel.WrapErr("mark confirm-fee wallet tx success", err)
		}
	}

	// Step 9: Initiate gateway payment for confirm fee (outside Run — cross-module + I/O).
	var gatewayURL *string
	if confirmFeeGateway > 0 {
		paymentClient, err := b.getPaymentClient(params.PaymentOption)
		if err != nil {
			return zero, sharedmodel.WrapErr("get payment client", err)
		}

		result, err := paymentClient.Create(ctx, payment.CreateParams{
			RefID:       runRes.BlockerTxID,
			Amount:      confirmFeeGateway,
			Description: fmt.Sprintf("Confirm fee tx %d", runRes.BlockerTxID),
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create gateway payment for confirm fee", err)
		}
		if result.RedirectURL != "" {
			gatewayURL = &result.RedirectURL

			// Persist the URL on tx.data so the seller can resume payment from
			// the pending list ("Continue Payment") even after the confirm
			// response is gone from memory.
			data, _ := json.Marshal(map[string]string{"gateway_url": result.RedirectURL})
			if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
				return b.storage.Querier().SetTransactionData(ctx, orderdb.SetTransactionDataParams{
					ID:   runRes.BlockerTxID,
					Data: data,
				})
			}); err != nil {
				return zero, sharedmodel.WrapErr("persist gateway url on tx", err)
			}
		}

		// Schedule timeout for the confirm-fee session.
		restate.ServiceSend(ctx, b.ServiceName(), "TimeoutConfirmFeeSession").Send(
			TimeoutConfirmFeeSessionParams{
				SessionID: runRes.ConfirmSession.ID,
				OrderID:   runRes.Order.ID,
			},
			restate.WithDelay(paymentExpiry),
		)
	}

	// Notify buyer (fire-and-forget).
	itemNames := lo.Map(orderItems, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
	summary := ordermodel.SummarizeNames(itemNames)
	notiMeta, _ := json.Marshal(map[string]string{"order_id": runRes.Order.ID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: buyerID,
		Type:      accountmodel.NotiItemsConfirmed,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Items confirmed",
		Content:   fmt.Sprintf("%s has been confirmed by the seller.", summary),
		Metadata:  notiMeta,
	})

	// Hydrate and return order.
	hydratedOrder, err := b.GetBuyerOrder(ctx, runRes.Order.ID)
	if err != nil {
		return zero, sharedmodel.WrapErr("get hydrated order", err)
	}

	return ConfirmSellerPendingResult{
		Order:                  hydratedOrder,
		ConfirmFeeTxIDs:        runRes.ConfirmFeeTxIDs,
		PayoutTxID:             runRes.PayoutTx.ID,
		BlockerTxID:            runRes.BlockerTxID,
		RequiresGatewayPayment: confirmFeeGateway > 0,
		GatewayURL:             gatewayURL,
	}, nil
}

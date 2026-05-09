package orderbiz

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// OnPaymentResult is the unified entry point for gateway IPN webhooks.
func (b *OrderHandler) OnPaymentResult(ctx restate.Context, params payment.Notification) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate on payment result", err)
	}

	txID, err := uuid.Parse(params.RefID)
	if err != nil {
		return sharedmodel.WrapErr("parse tx id", err)
	}

	tx, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderTransaction, error) {
		return b.storage.Querier().GetTransaction(rctx, uuid.NullUUID{UUID: txID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("get transaction", err)
	}

	// load session + resolve TxID if the webhook didn't supply one.
	session, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: tx.SessionID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("get session", err)
	}

	wfName, wfID := WorkflowForSession(session)
	if wfName == "" {
		return nil
	}

	// signal owning workflow's payment_event promise.
	restate.WorkflowSend(ctx, wfName, wfID, "PaymentNotification").Send(params)
	return nil
}

// WorkflowForSession maps payment_session.kind to (workflowName, workflowID).
func WorkflowForSession(s orderdb.OrderPaymentSession) (workflowName, workflowID string) {
	switch s.Kind {
	case SessionKindBuyerCheckout:
		return "CheckoutWorkflow", s.ID.String()
	case SessionKindSellerConfirmationFee:
		return "ConfirmWorkflow", s.ID.String()
	default:
		return "", ""
	}
}

type InitGatewayPaymentParams struct {
	TxID          uuid.UUID
	Amount        int64
	PaymentOption string
	Description   string
}

// InitGatewayPayment creates a gateway payment
func (b *OrderHandler) InitGatewayPayment(
	ctx restate.Context,
	params InitGatewayPaymentParams,
) (string, error) {
	if params.Amount <= 0 {
		return "", nil
	}

	paymentClient, err := b.getPaymentClient(params.PaymentOption)
	if err != nil {
		return "", sharedmodel.WrapErr("get payment client", err)
	}

	url, err := restate.Run(ctx, func(rctx restate.RunContext) (string, error) {
		r, e := paymentClient.Charge(rctx, payment.ChargeParams{
			RefID:       params.TxID.String(),
			Amount:      params.Amount,
			Description: params.Description,
		})
		if e != nil {
			return "", e
		}
		return r.RedirectURL, nil
	})
	if err != nil {
		return "", sharedmodel.WrapErr("create gateway payment", err)
	}

	if url != "" {
		if pErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			data, _ := json.Marshal(map[string]string{"gateway_url": url})
			return b.storage.Querier().SetTransactionData(rctx, orderdb.SetTransactionDataParams{
				ID:   params.TxID,
				Data: data,
			})
		}); pErr != nil {
			return "", sharedmodel.WrapErr("persist gateway url on tx", pErr)
		}
	}

	return url, nil
}

// GetReusableGatewayURL reports whether a checkout/confirm session has a
// Pending+not-expired gateway tx whose URL the client can reuse. The echo
// "ensure payment URL" handler uses this to skip a workflow round-trip on
// the happy path; on the retry path it falls back to RequestNewPaymentURL.
func (b *OrderHandler) GetReusableGatewayURL(
	ctx restate.Context,
	sessionID uuid.UUID,
) (ReusableGatewayURLState, error) {
	var state ReusableGatewayURLState

	session, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: sessionID, Valid: true})
	})
	if err != nil {
		return state, sharedmodel.WrapErr("get payment session", err)
	}
	if session.Status != orderdb.OrderStatusPending {
		state.SessionTerminated = true
		return state, nil
	}

	tx, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderTransaction, error) {
		return b.storage.Querier().GetLatestGatewayTxBySession(rctx, sessionID)
	})
	if err != nil {
		// pgx returns ErrNoRows when no gateway tx exists yet — treat as
		// "no reusable URL" so the caller signals the workflow.
		if errors.Is(err, pgx.ErrNoRows) {
			return state, nil
		}
		return state, sharedmodel.WrapErr("get latest gateway tx", err)
	}

	if tx.Status == orderdb.OrderStatusPending &&
		tx.DateExpired.Valid &&
		tx.DateExpired.Time.After(time.Now()) {
		var data struct {
			GatewayURL string `json:"gateway_url"`
		}
		if jerr := json.Unmarshal(tx.Data, &data); jerr == nil && data.GatewayURL != "" {
			state.ReusableURL = data.GatewayURL
		}
	}
	return state, nil
}

// gatewayPaymentLoopParams configures runGatewayPaymentLoop for a specific
// workflow (CheckoutWorkflow vs ConfirmWorkflow). Differences are pure data:
// amounts, currencies, error mapping, log strings.
type gatewayPaymentLoopParams struct {
	SessionID       uuid.UUID
	WorkflowID      uuid.UUID
	SessionDeadline time.Time

	NotePrefix    string // tx.note prefix, e.g. "checkout gateway payment"
	Description   string // gateway transaction memo, e.g. "Checkout session %s"
	PaymentOption string
	Amount        int64
	FromCurrency  string
	ToCurrency    string
	ExchangeRate  pgtype.Numeric

	ErrCancelled sharedmodel.Error
	ErrExpired   sharedmodel.Error
}

// runGatewayPaymentLoop drives the multi-attempt gateway payment leg shared
// by CheckoutWorkflow and ConfirmWorkflow. Each iteration: mints a fresh
// gateway tx (Pending, expires now+paymentExpiry), calls InitGatewayPayment,
// resolves payment_url_<attempt>. Then waits on:
//
//   - paymentPromise (payment_event_<txID>): gateway settled. On Success,
//     marks the tx + session Success and returns nil.
//   - cancelPromise:  buyer/seller signalled cancel → terminal ErrCancelled.
//   - attempt-expiry: this attempt's URL window elapsed → mark tx Failed,
//     wait for retry_<attempt> (resolved by RequestNewPaymentURL shared
//     handler) and loop into the next attempt.
//   - session-expiry: overall session deadline elapsed → terminal ErrExpired.
//
// Caller is responsible for: workflow-level saga registration, the
// pre-loop session/wallet-tx creation, and the post-success tail. This
// helper only owns the gateway leg.
func (b *OrderHandler) runGatewayPaymentLoop(
	ctx restate.WorkflowContext,
	p gatewayPaymentLoopParams,
) error {
	cancelPromise := restate.Promise[struct{}](ctx, "user_cancel")
	var attempt int

paymentLoop:
	for {
		attempt++
		restate.Set(ctx, "payment_attempt", attempt)
		attemptTxID := restate.UUID(ctx)

		if cErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			_, e := b.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				ID:            attemptTxID,
				SessionID:     p.SessionID,
				Status:        orderdb.OrderStatusPending,
				Note:          fmt.Sprintf("%s (attempt %d)", p.NotePrefix, attempt),
				Error:         null.String{},
				PaymentOption: null.StringFrom(p.PaymentOption),
				Data:          json.RawMessage("{}"),
				Amount:        p.Amount,
				FromCurrency:  p.FromCurrency,
				ToCurrency:    p.ToCurrency,
				ExchangeRate:  p.ExchangeRate,
				ReversesID:    uuid.NullUUID{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			})
			return e
		}); cErr != nil {
			return sharedmodel.WrapErr("db create gateway tx", cErr)
		}

		url, gErr := b.InitGatewayPayment(ctx, InitGatewayPaymentParams{
			TxID:          attemptTxID,
			Amount:        p.Amount,
			PaymentOption: p.PaymentOption,
			Description:   fmt.Sprintf("%s (attempt %d)", p.Description, attempt),
		})
		if gErr != nil {
			return gErr
		}
		if pErr := restate.Promise[string](ctx, fmt.Sprintf("payment_url_%d", attempt)).Resolve(url); pErr != nil {
			return sharedmodel.WrapErr("resolve payment url promise", pErr)
		}

		paymentPromise := restate.Promise[payment.Notification](ctx, "payment_event_"+attemptTxID.String())
		attemptExpiryFut := restate.After(ctx, paymentExpiry)
		sessionRem := time.Until(p.SessionDeadline)
		if sessionRem <= 0 {
			return p.ErrExpired.Terminal()
		}
		sessionExpiryFut := restate.After(ctx, sessionRem)

		done, werr := restate.WaitFirst(ctx, paymentPromise, cancelPromise, attemptExpiryFut, sessionExpiryFut)
		if werr != nil {
			return werr
		}
		switch done {
		case paymentPromise:
			ev, evErr := paymentPromise.Result()
			if evErr != nil {
				return sharedmodel.WrapErr("read payment event", evErr)
			}
			switch ev.Status {
			case payment.StatusSuccess:
				// Promote this attempt's gateway tx + the session to Success.
				// Both queries guard on status='Pending' so they're idempotent
				// on workflow replay.
				if mErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
					now := time.Now()
					if _, e := b.storage.Querier().MarkTransactionSuccess(rctx, orderdb.MarkTransactionSuccessParams{
						ID:          attemptTxID,
						DateSettled: now,
					}); e != nil {
						return sharedmodel.WrapErr("mark gateway tx success", e)
					}
					if _, e := b.storage.Querier().MarkPaymentSessionSuccess(rctx, orderdb.MarkPaymentSessionSuccessParams{
						ID:       p.SessionID,
						DatePaid: now,
					}); e != nil {
						return sharedmodel.WrapErr("mark payment session success", e)
					}
					return nil
				}); mErr != nil {
					return mErr
				}
				break paymentLoop
			case payment.StatusFailed, payment.StatusExpired:
				return ordermodel.ErrPaymentFailed.Terminal()
			default:
				return sharedmodel.WrapErr("unknown payment event status", ordermodel.ErrPaymentFailed.Terminal())
			}
		case cancelPromise:
			return p.ErrCancelled.Terminal()
		case sessionExpiryFut:
			return p.ErrExpired.Terminal()
		case attemptExpiryFut:
			// This attempt's URL is dead. Mark its tx Failed and wait for the
			// caller to ask for a fresh one (RequestNewPaymentURL resolves
			// retry_<attempt>). Lazy retry: we don't burn gateway quota until
			// the user actually comes back.
			if mErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				return b.storage.Querier().MarkTransactionsFailed(rctx, orderdb.MarkTransactionsFailedParams{
					ID:    []uuid.UUID{attemptTxID},
					Error: null.StringFrom("gateway attempt expired"),
				})
			}); mErr != nil {
				return sharedmodel.WrapErr("mark attempt failed", mErr)
			}

			retryPromise := restate.Promise[struct{}](ctx, fmt.Sprintf("retry_%d", attempt))
			sessionRem2 := time.Until(p.SessionDeadline)
			if sessionRem2 <= 0 {
				return p.ErrExpired.Terminal()
			}
			sessionExpiryFut2 := restate.After(ctx, sessionRem2)

			done2, werr2 := restate.WaitFirst(ctx, retryPromise, cancelPromise, sessionExpiryFut2)
			if werr2 != nil {
				return werr2
			}
			switch done2 {
			case retryPromise:
				continue paymentLoop
			case cancelPromise:
				return p.ErrCancelled.Terminal()
			case sessionExpiryFut2:
				return p.ErrExpired.Terminal()
			}
		}
	}
	return nil
}

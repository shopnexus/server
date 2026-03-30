package orderbiz

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"

	accountbiz "shopnexus-server/internal/module/account/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// orderInfo is a JSON-safe struct for restate.Run journal serialization.
type orderInfo struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
}

// PayBuyerOrders creates a payment for one or more unpaid orders belonging to the buyer.
func (b *OrderHandler) PayBuyerOrders(ctx restate.Context, params PayBuyerOrdersParams) (PayBuyerOrdersResult, error) {
	var zero PayBuyerOrdersResult

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate pay orders", err)
	}

	// Fetch orders and validate
	type fetchResult struct {
		Orders []orderInfo `json:"orders"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		orders, err := b.storage.Querier().ListOrder(ctx, orderdb.ListOrderParams{
			ID: params.OrderIDs,
		})
		if err != nil {
			return fetchResult{}, sharedmodel.WrapErr("db list orders", err)
		}
		if len(orders) != len(params.OrderIDs) {
			return fetchResult{}, ordermodel.ErrOrderNotFound.Terminal()
		}

		var result []orderInfo
		for _, o := range orders {
			if o.BuyerID != params.Account.ID {
				return fetchResult{}, ordermodel.ErrOrderNotFound.Terminal()
			}
			if o.Status != orderdb.OrderStatusPending {
				return fetchResult{}, ordermodel.ErrOrderNotPayable
			}
			if o.PaymentID.Valid {
				return fetchResult{}, ordermodel.ErrOrderAlreadyPaid
			}
			result = append(result, orderInfo{
				ID:    o.ID.String(),
				Total: o.Total,
			})
		}
		return fetchResult{Orders: result}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch orders", err)
	}

	var totalAmount sharedmodel.Concurrency
	for _, o := range fetched.Orders {
		totalAmount += sharedmodel.Concurrency(o.Total)
	}

	// Branch based on payment option
	option := params.PaymentOption
	if strings.HasPrefix(option, "pm:") {
		return b.payWithSavedMethod(ctx, params, option[3:], totalAmount)
	}
	if option == "" || option == "default" {
		// Look up buyer's default payment method
		pmList, err := b.account.ListPaymentMethod(ctx, accountbiz.ListPaymentMethodParams{
			Account: params.Account,
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("list payment methods", err)
		}
		for _, pm := range pmList.Data {
			if pm.IsDefault {
				return b.payWithSavedMethod(ctx, params, pm.ID.String(), totalAmount)
			}
		}
		return zero, ordermodel.ErrNoDefaultPaymentMethod.Terminal()
	}
	return b.payWithRedirect(ctx, params, option, totalAmount)
}

// payWithRedirect handles redirect-based payments (VNPay, SePay, COD).
func (b *OrderHandler) payWithRedirect(ctx restate.Context, params PayBuyerOrdersParams, option string, totalAmount sharedmodel.Concurrency) (PayBuyerOrdersResult, error) {
	var zero PayBuyerOrdersResult

	paymentClient, err := b.getPaymentClient(option)
	if err != nil {
		return zero, err
	}

	type paymentResult struct {
		PaymentID   int64  `json:"payment_id"`
		RedirectURL string `json:"redirect_url"`
	}

	payInfo, err := restate.Run(ctx, func(ctx restate.RunContext) (paymentResult, error) {
		expiryDays := b.config.App.Order.PaymentExpiryDays
		if expiryDays <= 0 {
			expiryDays = 30
		}

		// Create payment record first to get the ID for the provider
		dbPayment, err := b.storage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
			AccountID:   params.Account.ID,
			Option:      option,
			Amount:      int64(totalAmount),
			Data:        []byte("{}"),
			DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
		})
		if err != nil {
			return paymentResult{}, sharedmodel.WrapErr("db create payment", err)
		}

		createdOrder, err := paymentClient.Create(ctx, payment.CreateParams{
			RefID:       dbPayment.ID,
			Amount:      totalAmount,
			Description: fmt.Sprintf("Payment %d", dbPayment.ID),
		})
		if err != nil {
			return paymentResult{}, sharedmodel.WrapErr("create payment order", err)
		}

		// Store redirect URL in payment data so buyer can re-open it later
		if createdOrder.RedirectURL != "" {
			data, _ := json.Marshal(map[string]string{"redirect_url": createdOrder.RedirectURL})
			_, _ = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
				ID:   dbPayment.ID,
				Data: data,
			})
		}

		return paymentResult{
			PaymentID:   dbPayment.ID,
			RedirectURL: createdOrder.RedirectURL,
		}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create payment", err)
	}

	// Link payment to all orders
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		return b.storage.Querier().SetOrderPayment(ctx, orderdb.SetOrderPaymentParams{
			PaymentID: null.IntFrom(payInfo.PaymentID),
			Ids:       params.OrderIDs,
			BuyerID:   params.Account.ID,
		})
	}); err != nil {
		return zero, sharedmodel.WrapErr("db set order payment", err)
	}

	return b.fetchPaymentResult(ctx, payInfo.PaymentID, payInfo.RedirectURL)
}

// payWithSavedMethod handles charging a saved card.
// TODO: Complete implementation after account payment method biz is wired (Task 7).
func (b *OrderHandler) payWithSavedMethod(ctx restate.Context, params PayBuyerOrdersParams, paymentMethodID string, totalAmount sharedmodel.Concurrency) (PayBuyerOrdersResult, error) {
	return PayBuyerOrdersResult{}, sharedmodel.NewError(501, "saved card payment not yet implemented").Terminal()
}

// fetchPaymentResult fetches the created payment and builds the response.
func (b *OrderHandler) fetchPaymentResult(ctx restate.Context, paymentID int64, redirectURL string) (PayBuyerOrdersResult, error) {
	var zero PayBuyerOrdersResult

	paymentModel, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Payment, error) {
		dbPay, err := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: []int64{paymentID},
		})
		if err != nil || len(dbPay) == 0 {
			return ordermodel.Payment{}, sharedmodel.WrapErr("db fetch payment", err)
		}
		p := dbPay[0]
		var datePaid *time.Time
		if p.DatePaid.Valid {
			datePaid = &p.DatePaid.Time
		}
		var pmID *uuid.UUID
		if p.PaymentMethodID.Valid {
			pmID = &p.PaymentMethodID.UUID
		}
		return ordermodel.Payment{
			ID:              p.ID,
			AccountID:       p.AccountID,
			Option:          p.Option,
			PaymentMethodID: pmID,
			Status:          p.Status,
			Amount:          sharedmodel.Concurrency(p.Amount),
			Data:            p.Data,
			DateCreated:     p.DateCreated,
			DatePaid:        datePaid,
			DateExpired:     p.DateExpired,
		}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch payment", err)
	}

	var redirectUrl *string
	if redirectURL != "" {
		redirectUrl = &redirectURL
	}

	return PayBuyerOrdersResult{
		Payment:     paymentModel,
		RedirectUrl: redirectUrl,
	}, nil
}

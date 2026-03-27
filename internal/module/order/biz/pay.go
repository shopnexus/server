package orderbiz

import (
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/payment"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/guregu/null/v6"
)

// PayOrders creates a payment for one or more unpaid orders belonging to the buyer.
func (b *OrderHandler) PayOrders(ctx restate.Context, params PayOrdersParams) (PayOrdersResult, error) {
	var zero PayOrdersResult

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate pay orders", err)
	}

	// Validate payment option exists
	paymentClient, err := b.getPaymentClient(params.PaymentOption)
	if err != nil {
		return zero, err
	}

	// Fetch orders and validate
	type orderInfo struct {
		ID    string `json:"id"`
		Total int64  `json:"total"`
	}
	type fetchResult struct {
		Orders []orderInfo `json:"orders"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		orders, err := b.storage.Querier().ListOrder(ctx, orderdb.ListOrderParams{
			ID: params.OrderIDs,
		})
		if err != nil {
			return fetchResult{}, fmt.Errorf("list orders: %w", err)
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

	// Calculate total amount
	var totalAmount sharedmodel.Concurrency
	for _, o := range fetched.Orders {
		totalAmount += sharedmodel.Concurrency(o.Total)
	}

	// Create payment record + call payment provider
	type paymentResult struct {
		PaymentID   int64  `json:"payment_id"`
		RedirectURL string `json:"redirect_url"`
	}

	payInfo, err := restate.Run(ctx, func(ctx restate.RunContext) (paymentResult, error) {
		expiryDays := config.GetConfig().App.Order.PaymentExpiryDays
		if expiryDays <= 0 {
			expiryDays = 30
		}

		dbPayment, err := b.storage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
			AccountID:   params.Account.ID,
			Option:      params.PaymentOption,
			Amount:      int64(totalAmount),
			Data:        []byte("[]"),
			DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
		})
		if err != nil {
			return paymentResult{}, fmt.Errorf("create payment: %w", err)
		}

		createdOrder, err := paymentClient.CreateOrder(ctx, payment.CreateOrderParams{
			RefID:  dbPayment.ID,
			Amount: totalAmount,
			Info:   fmt.Sprintf("Payment %d", dbPayment.ID),
		})
		if err != nil {
			return paymentResult{}, fmt.Errorf("create payment order: %w", err)
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
		return zero, sharedmodel.WrapErr("set order payment", err)
	}

	// Fetch created payment for response
	paymentModel, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Payment, error) {
		dbPay, err := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: []int64{payInfo.PaymentID},
		})
		if err != nil || len(dbPay) == 0 {
			return ordermodel.Payment{}, fmt.Errorf("fetch payment: %w", err)
		}
		p := dbPay[0]
		var datePaid *time.Time
		if p.DatePaid.Valid {
			datePaid = &p.DatePaid.Time
		}
		return ordermodel.Payment{
			ID:          p.ID,
			AccountID:   p.AccountID,
			Option:      p.Option,
			Status:      p.Status,
			Amount:      sharedmodel.Concurrency(p.Amount),
			Data:        p.Data,
			DateCreated: p.DateCreated,
			DatePaid:    datePaid,
			DateExpired: p.DateExpired,
		}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch payment", err)
	}

	var redirectUrl *string
	if payInfo.RedirectURL != "" {
		redirectUrl = &payInfo.RedirectURL
	}

	return PayOrdersResult{
		Payment:     paymentModel,
		RedirectUrl: redirectUrl,
	}, nil
}

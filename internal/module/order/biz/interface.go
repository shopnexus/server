package orderbiz

import (
	"context"
	"errors"

	"shopnexus-server/internal/infras/payment"
	"shopnexus-server/internal/infras/pubsub"
	"shopnexus-server/internal/infras/shipment"
	accountbiz "shopnexus-server/internal/module/account/biz"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	commonbiz "shopnexus-server/internal/module/common/biz"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// OrderClient is the client interface for OrderBiz, which is used by other modules to call OrderBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface OrderClient -service OrderBiz
type OrderClient interface {
	// Order
	GetOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListOrders(ctx context.Context, params ListOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	QuoteOrder(ctx context.Context, params QuoteOrderParams) (QuoteOrderResult, error)
	VerifyPayment(ctx context.Context, params VerifyPaymentParams) error
	Checkout(ctx context.Context, params CheckoutParams) (CheckoutResult, error)
	CancelOrder(ctx context.Context, params CancelOrderParams) error

	// Vendor
	ListVendorOrder(ctx context.Context, params ListVendorOrderParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	ConfirmOrder(ctx context.Context, params ConfirmOrderParams) error

	// Cart
	GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
	UpdateCart(ctx context.Context, params UpdateCartParams) error
	ClearCart(ctx context.Context, params ClearCartParams) error
	ListCheckoutCart(ctx context.Context, params ListCheckoutCartParams) ([]ordermodel.CartItem, error)

	// Refund
	ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error)
	UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error)
	CancelRefund(ctx context.Context, params CancelRefundParams) error
	ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error)
}

type OrderStorage = pgsqlc.Storage[*orderdb.Queries]

type OrderBiz struct {
	storage     OrderStorage
	paymentMap  map[string]payment.Client  // map[paymentOption]payment.Client
	shipmentMap map[string]shipment.Client // map[shipmentOption]shipment.Client
	pubsub      pubsub.Client
	account     *accountbiz.AccountBiz
	catalog     *catalogbiz.CatalogBiz
	inventory   *inventorybiz.InventoryBiz
	promotion   *promotionbiz.PromotionBiz
	common      *commonbiz.CommonBiz
	analytic    *analyticbiz.AnalyticBiz
}

func NewOrderBiz(
	storage OrderStorage,
	pubsub pubsub.Client,
	account *accountbiz.AccountBiz,
	catalog *catalogbiz.CatalogBiz,
	inventory *inventorybiz.InventoryBiz,
	promotion *promotionbiz.PromotionBiz,
	common *commonbiz.CommonBiz,
	analytic *analyticbiz.AnalyticBiz,
) (*OrderBiz, error) {
	b := &OrderBiz{
		storage:   storage,
		pubsub:    pubsub.Group("order"),
		account:   account,
		catalog:   catalog,
		inventory: inventory,
		promotion: promotion,
		common:    common,
		analytic:  analytic,
	}

	return b, errors.Join(
		b.SetupPaymentMap(),
		b.SetupShipmentMap(),
		b.SetupPubsub(),
	)
}

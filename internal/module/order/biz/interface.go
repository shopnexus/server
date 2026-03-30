package orderbiz

import (
	"context"
	"errors"
	"time"

	"shopnexus-server/config"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	commonbiz "shopnexus-server/internal/module/common/biz"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

// OrderBiz is the client interface for OrderHandler, which is used by other modules to call OrderHandler methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface OrderBiz -service Order
type OrderBiz interface {
	// Checkout
	BuyerCheckout(ctx context.Context, params BuyerCheckoutParams) (BuyerCheckoutResult, error)

	// Pending Items (buyer)
	ListBuyerPending(ctx context.Context, params ListBuyerPendingParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	CancelBuyerPending(ctx context.Context, params CancelBuyerPendingParams) error

	// Incoming Items (seller)
	ListSellerPending(ctx context.Context, params ListSellerPendingParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	ConfirmSellerPending(ctx context.Context, params ConfirmSellerPendingParams) (ordermodel.Order, error)
	RejectSellerPending(ctx context.Context, params RejectSellerPendingParams) error

	// Orders
	GetBuyerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	GetSellerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListBuyerConfirmed(ctx context.Context, params ListBuyerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	ListSellerConfirmed(ctx context.Context, params ListSellerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	CancelBuyerOrder(ctx context.Context, params CancelBuyerOrderParams) error

	// Payment
	PayBuyerOrders(ctx context.Context, params PayBuyerOrdersParams) (PayBuyerOrdersResult, error)
	ConfirmPayment(ctx context.Context, params ConfirmPaymentParams) error

	// Cart (unchanged)
	GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
	UpdateCart(ctx context.Context, params UpdateCartParams) error
	ClearCart(ctx context.Context, params ClearCartParams) error

	// Review eligibility
	HasPurchasedProduct(ctx context.Context, params HasPurchasedProductParams) (bool, error)
	ListReviewableOrders(ctx context.Context, params ListReviewableOrdersParams) ([]ReviewableOrder, error)
	ValidateOrderForReview(ctx context.Context, params ValidateOrderForReviewParams) (bool, error)

	// Refund
	ListBuyerRefunds(ctx context.Context, params ListBuyerRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	CreateBuyerRefund(ctx context.Context, params CreateBuyerRefundParams) (ordermodel.Refund, error)
	UpdateBuyerRefund(ctx context.Context, params UpdateBuyerRefundParams) (ordermodel.Refund, error)
	CancelBuyerRefund(ctx context.Context, params CancelBuyerRefundParams) error
	ConfirmSellerRefund(ctx context.Context, params ConfirmSellerRefundParams) (ordermodel.Refund, error)

	// Dashboard
	GetSellerOrderStats(ctx context.Context, params GetSellerOrderStatsParams) (SellerOrderStats, error)
	GetSellerOrderTimeSeries(ctx context.Context, params GetSellerOrderTimeSeriesParams) ([]SellerOrderTimeSeriesPoint, error)
	GetSellerPendingActions(ctx context.Context, params GetSellerPendingActionsParams) (SellerPendingActions, error)
	GetSellerTopProducts(ctx context.Context, params GetSellerTopProductsParams) ([]SellerTopProduct, error)
}

type OrderStorage = pgsqlc.Storage[*orderdb.Queries]

// OrderHandler implements the core business logic for the order module.
type OrderHandler struct {
	config       *config.Config
	storage      OrderStorage
	paymentMap   map[string]payment.Client
	transportMap map[string]transport.Client
	account      accountbiz.AccountBiz
	catalog      catalogbiz.CatalogBiz
	inventory    inventorybiz.InventoryBiz
	promotion    promotionbiz.PromotionBiz
	common       commonbiz.CommonBiz
}

func (b *OrderHandler) ServiceName() string {
	return "Order"
}

// PaymentClients returns the registered payment clients.
func (b *OrderHandler) PaymentClients() map[string]payment.Client {
	return b.paymentMap
}

// NewOrderHandler creates a new OrderHandler with the given dependencies.
func NewOrderHandler(
	cfg *config.Config,
	storage OrderStorage,
	account accountbiz.AccountBiz,
	catalog catalogbiz.CatalogBiz,
	inventory inventorybiz.InventoryBiz,
	promotion promotionbiz.PromotionBiz,
	common commonbiz.CommonBiz,
) (*OrderHandler, error) {
	b := &OrderHandler{
		config:    cfg,
		storage:   storage,
		account:   account,
		catalog:   catalog,
		inventory: inventory,
		promotion: promotion,
		common:    common,
	}

	return b, errors.Join(
		b.SetupPaymentMap(),
		b.SetupTransportMap(),
	)
}

// --- Param structs ---

type BuyerCheckoutParams struct {
	Account accountmodel.AuthenticatedAccount
	BuyNow  bool           `validate:"omitempty"`
	Items   []CheckoutItem `validate:"required,min=1,dive"`
}

type CheckoutItem struct {
	SkuID    uuid.UUID `json:"sku_id" validate:"required"`
	Quantity int64     `json:"quantity" validate:"required,gt=0"`
	Address  string    `json:"address" validate:"required,min=1,max=500"`
	Note     string    `json:"note" validate:"max=500"`
}

type BuyerCheckoutResult struct {
	Items []ordermodel.OrderItem `json:"items"`
}

type ListBuyerPendingParams struct {
	AccountID uuid.UUID                 `validate:"required"`
	Status    []orderdb.OrderItemStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}

type CancelBuyerPendingParams struct {
	AccountID uuid.UUID `validate:"required"`
	ItemID    int64     `validate:"required"`
}

type ListSellerPendingParams struct {
	SellerID uuid.UUID   `validate:"required"`
	Search   null.String `validate:"omitnil"`
	sharedmodel.PaginationParams
}

type ConfirmSellerPendingParams struct {
	Account         accountmodel.AuthenticatedAccount
	ItemIDs         []int64 `validate:"required,min=1"`
	TransportOption string  `validate:"required,min=1,max=100"`
	Note            string  `validate:"max=500"`
}

type RejectSellerPendingParams struct {
	Account accountmodel.AuthenticatedAccount
	ItemIDs []int64 `validate:"required,min=1"`
}

type ListBuyerConfirmedParams struct {
	sharedmodel.PaginationParams
	ID []uuid.UUID `validate:"dive"`
}

type ListSellerConfirmedParams struct {
	SellerID      uuid.UUID             `validate:"required"`
	Search        null.String           `validate:"omitnil"`
	PaymentStatus []orderdb.OrderStatus `validate:"omitempty"`
	OrderStatus   []orderdb.OrderStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}

type CancelBuyerOrderParams struct {
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID
}

type PayBuyerOrdersParams struct {
	Account       accountmodel.AuthenticatedAccount
	OrderIDs      []uuid.UUID `validate:"required,min=1"`
	PaymentOption string      `validate:"max=100"`
}

type PayBuyerOrdersResult struct {
	Payment     ordermodel.Payment `json:"payment"`
	RedirectUrl *string            `json:"redirect_url"`
}

type ConfirmPaymentParams struct {
	RefID  string         `validate:"required"`
	Status payment.Status `validate:"required"`
}

type GetCartParams struct {
	AccountID uuid.UUID `validate:"required"`
}

type UpdateCartParams struct {
	Account accountmodel.AuthenticatedAccount

	SkuID         uuid.UUID  `validate:"required"`
	Quantity      null.Int64 `validate:"omitnil,min=0,max=1000"`
	DeltaQuantity null.Int64 `validate:"omitnil,min=1,max=1000"`
}

type ClearCartParams struct {
	Account accountmodel.AuthenticatedAccount
}

type ListBuyerRefundsParams struct {
	sharedmodel.PaginationParams
}

type CreateBuyerRefundParams struct {
	Account     accountmodel.AuthenticatedAccount
	OrderID     uuid.UUID                 `validate:"required"`
	Method      orderdb.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string                    `validate:"required,max=500"`
	Address     null.String               `validate:"omitempty,max=500"`
	ResourceIDs []uuid.UUID               `validate:"dive"`
}

type UpdateBuyerRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID                 `validate:"required"`
	Method   orderdb.OrderRefundMethod `validate:"omitempty,validateFn=Valid"`
	Address  null.String               `validate:"omitnil,max=500"`
	Reason   null.String               `validate:"omitnil,max=500"`

	// Fields below are only updated after vendor confirms
	Status        orderdb.OrderStatus `validate:"omitempty,validateFn=Valid"`
	ConfirmedByID uuid.NullUUID       `validate:"omitnil"`
	ResourceIDs   []uuid.UUID         `validate:"required,dive"`
}

type CancelBuyerRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

type HasPurchasedProductParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids" validate:"required,min=1"`
}

type ListReviewableOrdersParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids" validate:"required,min=1"`
}

type ReviewableOrder struct {
	ID          uuid.UUID `json:"id"`
	Total       int64     `json:"total"`
	DateCreated time.Time `json:"date_created"`
}

type ValidateOrderForReviewParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	OrderID   uuid.UUID   `json:"order_id" validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids" validate:"required,min=1"`
}

type ConfirmSellerRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

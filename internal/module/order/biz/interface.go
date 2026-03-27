package orderbiz

import (
	"context"
	"errors"

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
	Checkout(ctx context.Context, params CheckoutParams) (CheckoutResult, error)

	// Pending Items (buyer)
	ListPendingItems(ctx context.Context, params ListPendingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	CancelPendingItem(ctx context.Context, params CancelPendingItemParams) error

	// Incoming Items (seller)
	ListIncomingItems(ctx context.Context, params ListIncomingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	ConfirmItems(ctx context.Context, params ConfirmItemsParams) (ordermodel.Order, error)
	RejectItems(ctx context.Context, params RejectItemsParams) error

	// Orders
	GetOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListOrders(ctx context.Context, params ListOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	ListSellerOrders(ctx context.Context, params ListSellerOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	CancelOrder(ctx context.Context, params CancelOrderParams) error

	// Payment
	PayOrders(ctx context.Context, params PayOrdersParams) (PayOrdersResult, error)
	VerifyPayment(ctx context.Context, params VerifyPaymentParams) error

	// Cart (unchanged)
	GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
	UpdateCart(ctx context.Context, params UpdateCartParams) error
	ClearCart(ctx context.Context, params ClearCartParams) error

	// Refund
	ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error)
	UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error)
	CancelRefund(ctx context.Context, params CancelRefundParams) error
	ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error)
}

type OrderStorage = pgsqlc.Storage[*orderdb.Queries]

// OrderHandler implements the core business logic for the order module.
type OrderHandler struct {
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

// NewOrderHandler creates a new OrderHandler with the given dependencies.
func NewOrderHandler(
	storage OrderStorage,
	account accountbiz.AccountBiz,
	catalog catalogbiz.CatalogBiz,
	inventory inventorybiz.InventoryBiz,
	promotion promotionbiz.PromotionBiz,
	common commonbiz.CommonBiz,
) (*OrderHandler, error) {
	b := &OrderHandler{
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

type CheckoutParams struct {
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

type CheckoutResult struct {
	Items []ordermodel.OrderItem `json:"items"`
}

type ListPendingItemsParams struct {
	AccountID uuid.UUID                 `validate:"required"`
	Status    []orderdb.OrderItemStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}

type CancelPendingItemParams struct {
	AccountID uuid.UUID `validate:"required"`
	ItemID    int64     `validate:"required"`
}

type ListIncomingItemsParams struct {
	SellerID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type ConfirmItemsParams struct {
	Account         accountmodel.AuthenticatedAccount
	ItemIDs         []int64 `validate:"required,min=1"`
	TransportOption string  `validate:"required,min=1,max=100"`
	Note            string  `validate:"max=500"`
}

type RejectItemsParams struct {
	Account accountmodel.AuthenticatedAccount
	ItemIDs []int64 `validate:"required,min=1"`
}

type ListOrdersParams struct {
	sharedmodel.PaginationParams
	ID []uuid.UUID `validate:"dive"`
}

type ListSellerOrdersParams struct {
	SellerID      uuid.UUID             `validate:"required"`
	PaymentStatus []orderdb.OrderStatus `validate:"omitempty"`
	OrderStatus   []orderdb.OrderStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}

type CancelOrderParams struct {
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID
}

type PayOrdersParams struct {
	Account       accountmodel.AuthenticatedAccount
	OrderIDs      []uuid.UUID `validate:"required,min=1"`
	PaymentOption string      `validate:"required,min=1,max=100"`
}

type PayOrdersResult struct {
	Payment     ordermodel.Payment `json:"payment"`
	RedirectUrl *string            `json:"redirect_url"`
}

type VerifyPaymentParams struct {
	PaymentGateway string `validate:"required,min=1,max=50"`
	Data           map[string]any
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

type ListRefundsParams struct {
	sharedmodel.PaginationParams
}

type CreateRefundParams struct {
	Account     accountmodel.AuthenticatedAccount
	OrderID     uuid.UUID                 `validate:"required"`
	Method      orderdb.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string                    `validate:"required,max=500"`
	Address     null.String               `validate:"omitempty,max=500"`
	ResourceIDs []uuid.UUID               `validate:"dive"`
}

type UpdateRefundParams struct {
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

type CancelRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

type ConfirmRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

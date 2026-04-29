package orderbiz

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/locker"
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
	// Pending Items (buyer)
	ListBuyerPendingItems(
		ctx context.Context,
		params ListBuyerPendingItemsParams,
	) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	CancelBuyerPending(ctx context.Context, params CancelBuyerPendingParams) error

	// Incoming Items (seller)
	ListSellerPendingItems(
		ctx context.Context,
		params ListSellerPendingItemsParams,
	) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	RejectSellerPending(ctx context.Context, params RejectSellerPendingParams) error

	// Orders
	GetBuyerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	GetSellerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListBuyerConfirmed(
		ctx context.Context,
		params ListBuyerConfirmedParams,
	) (sharedmodel.PaginateResult[ordermodel.Order], error)
	ListSellerConfirmed(
		ctx context.Context,
		params ListSellerConfirmedParams,
	) (sharedmodel.PaginateResult[ordermodel.Order], error)

	// Payment webhook entrypoint — gateway providers and internal callers
	// route through here. MarkTxSuccess/MarkTxFailed are package-internal helpers.
	OnPaymentResult(ctx context.Context, params OnPaymentResultParams) error

	// Cart
	GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
	UpdateCart(ctx context.Context, params UpdateCartParams) error
	ClearCart(ctx context.Context, params ClearCartParams) error

	// Review eligibility
	HasPurchasedProduct(ctx context.Context, params HasPurchasedProductParams) (bool, error)
	ListReviewableOrders(ctx context.Context, params ListReviewableOrdersParams) ([]ReviewableOrder, error)
	ValidateOrderForReview(ctx context.Context, params ValidateOrderForReviewParams) (bool, error)

	// Refund (2-stage)
	ListBuyerRefunds(
		ctx context.Context,
		params ListBuyerRefundsParams,
	) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	ListSellerRefunds(
		ctx context.Context,
		params ListSellerRefundsParams,
	) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	CreateBuyerRefund(ctx context.Context, params CreateBuyerRefundParams) (ordermodel.Refund, error)
	AcceptRefundStage1(ctx context.Context, params AcceptRefundStage1Params) (ordermodel.Refund, error)
	ApproveRefundStage2(ctx context.Context, params ApproveRefundStage2Params) (ordermodel.Refund, error)
	RejectRefund(ctx context.Context, params RejectRefundParams) (ordermodel.Refund, error)

	// Dispute
	CreateRefundDispute(ctx context.Context, params CreateRefundDisputeParams) (ordermodel.RefundDispute, error)
	ListRefundDisputes(
		ctx context.Context,
		params ListRefundDisputesParams,
	) (sharedmodel.PaginateResult[ordermodel.RefundDispute], error)
	GetRefundDispute(ctx context.Context, params GetRefundDisputeParams) (ordermodel.RefundDispute, error)

	// Transport
	OnTransportResult(ctx context.Context, params OnTransportResultParams) error

	// Dashboard
	GetSellerOrderStats(ctx context.Context, params GetSellerOrderStatsParams) (SellerOrderStats, error)
	GetSellerOrderTimeSeries(
		ctx context.Context,
		params GetSellerOrderTimeSeriesParams,
	) ([]SellerOrderTimeSeriesPoint, error)
	GetSellerPendingActions(ctx context.Context, params GetSellerPendingActionsParams) (SellerPendingActions, error)
	GetSellerTopProducts(ctx context.Context, params GetSellerTopProductsParams) ([]SellerTopProduct, error)
}

type OrderStorage = pgsqlc.Storage[*orderdb.Queries]

// OrderHandler implements the core business logic for the order module.
type OrderHandler struct {
	config       *config.Config
	storage      OrderStorage
	locker       locker.Client
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

// TransportClients returns the registered transport clients.
func (b *OrderHandler) TransportClients() map[string]transport.Client {
	return b.transportMap
}

// NewOrderHandler creates a new OrderHandler with the given dependencies.
func NewOrderHandler(
	cfg *config.Config,
	storage OrderStorage,
	locker locker.Client,
	account accountbiz.AccountBiz,
	catalog catalogbiz.CatalogBiz,
	inventory inventorybiz.InventoryBiz,
	promotion promotionbiz.PromotionBiz,
	common commonbiz.CommonBiz,
) (*OrderHandler, error) {
	b := &OrderHandler{
		config:    cfg,
		storage:   storage,
		locker:    locker,
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

type CheckoutItem struct {
	SkuID           uuid.UUID `json:"sku_id" validate:"required"`
	Quantity        int64     `json:"quantity" validate:"required,gt=0,max=100000"`
	TransportOption string    `json:"transport_option" validate:"required,min=1,max=100"`
	Note            string    `json:"note" validate:"max=500"`
}

type ListBuyerPendingItemsParams struct {
	AccountID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type CancelBuyerPendingParams struct {
	AccountID uuid.UUID `validate:"required"`
	ItemID    int64     `validate:"required"`
}

type ListSellerPendingItemsParams struct {
	SellerID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type RejectSellerPendingParams struct {
	Account accountmodel.AuthenticatedAccount
	ItemIDs []int64 `validate:"required,min=1,max=1000"`
}

type ListBuyerConfirmedParams struct {
	BuyerID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type ListSellerConfirmedParams struct {
	SellerID uuid.UUID   `validate:"required"`
	Search   null.String `validate:"omitnil"`
	sharedmodel.PaginationParams
}

// --- Transaction internal helpers (used by OnPaymentResult and workflows) ---

type markTxSuccessParams struct {
	TxID   int64     `json:"tx_id" validate:"required"`
	DateAt time.Time `json:"date_at"`
}

type markTxFailedParams struct {
	TxID   int64  `json:"tx_id" validate:"required"`
	Reason string `json:"reason"`
}

// --- Cart ---

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

// --- Refund 2-stage ---

type ListBuyerRefundsParams struct {
	BuyerID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type ListSellerRefundsParams struct {
	SellerID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

type CreateBuyerRefundParams struct {
	Account               accountmodel.AuthenticatedAccount
	OrderID               uuid.UUID                 `json:"order_id" validate:"required"`
	Method                orderdb.OrderRefundMethod `json:"method" validate:"required,validateFn=Valid"`
	Reason                string                    `json:"reason" validate:"required,max=500"`
	Address               string                    `json:"address" validate:"omitempty,max=500"`
	ReturnTransportOption string                    `json:"return_transport_option" validate:"max=100"`
}

type AcceptRefundStage1Params struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `json:"refund_id" validate:"required"`
}

type ApproveRefundStage2Params struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `json:"refund_id" validate:"required"`
}

type RejectRefundParams struct {
	Account       accountmodel.AuthenticatedAccount
	RefundID      uuid.UUID `json:"refund_id" validate:"required"`
	Stage         int       `json:"stage" validate:"required,oneof=1 2"`
	RejectionNote string    `json:"rejection_note" validate:"required,min=1,max=1000"`
}

// --- Review eligibility ---

type HasPurchasedProductParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids"    validate:"required,min=1"`
}

type ListReviewableOrdersParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids"    validate:"required,min=1"`
}

type ReviewableOrder struct {
	ID          uuid.UUID `json:"id"`
	DateCreated time.Time `json:"date_created"`
}

type ValidateOrderForReviewParams struct {
	AccountID uuid.UUID   `json:"account_id" validate:"required"`
	OrderID   uuid.UUID   `json:"order_id"   validate:"required"`
	SkuIDs    []uuid.UUID `json:"sku_ids"    validate:"required,min=1"`
}

// --- Dispute ---

type CreateRefundDisputeParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
	Reason   string    `validate:"required,min=1,max=1000"`
	Note     string    `validate:"required,min=1,max=2000"`
}

type ListRefundDisputesParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.NullUUID `validate:"omitnil"`
	sharedmodel.PaginationParams
}

type GetRefundDisputeParams struct {
	Account   accountmodel.AuthenticatedAccount
	DisputeID uuid.UUID `validate:"required"`
}

// --- Transport ---

type OnTransportResultParams struct {
	TrackingID string              `validate:"omitempty"`
	Status     orderdb.OrderStatus `validate:"required,validateFn=Valid"`
	Data       json.RawMessage     `validate:"omitempty"`
}

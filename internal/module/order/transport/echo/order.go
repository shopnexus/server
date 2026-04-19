package orderecho

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"shopnexus-server/internal/infras/ratelimit"
	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

// Handler handles HTTP requests for the order module.
type Handler struct {
	biz orderbiz.OrderBiz
}

// NewHandler registers order module routes and returns the handler.
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz, handler *orderbiz.OrderHandler, rl *ratelimit.Factory) *Handler {
	h := &Handler{biz: biz}
	g := e.Group("/api/v1/order")

	// Per-endpoint rate limits on write-heavy / abuse-prone operations. Read
	// endpoints are uncapped. Limits are per authenticated account (or IP if
	// unauthenticated) and reset every minute.
	rlCheckout := rl.Middleware("checkout", 10, time.Minute)
	rlRefund := rl.Middleware("refund", 5, time.Minute)
	rlDispute := rl.Middleware("dispute", 3, time.Minute)

	// Cart (unchanged)
	g.GET("/cart", h.GetCart)
	g.POST("/cart", h.UpdateCart)
	g.DELETE("/cart", h.ClearCart)

	// Buyer - Pending
	g.POST("/buyer/checkout", h.BuyerCheckout, rlCheckout)
	g.GET("/buyer/pending", h.ListBuyerPendingItems)
	g.DELETE("/buyer/pending/:id", h.CancelBuyerPending)

	// Buyer - Confirmed
	g.GET("/buyer/confirmed", h.ListBuyerConfirmed)
	g.GET("/buyer/confirmed/:id", h.GetBuyerOrder)

	// Buyer - Refund
	buyerRefund := g.Group("/buyer/refund")
	buyerRefund.GET("", h.ListBuyerRefunds)
	buyerRefund.POST("", h.CreateBuyerRefund, rlRefund)
	buyerRefund.PATCH("", h.UpdateBuyerRefund)
	buyerRefund.DELETE("", h.CancelBuyerRefund)

	// Seller - Pending
	// TODO: add casbin role middleware for /seller/* routes
	g.GET("/seller/pending", h.ListSellerPendingItems)
	g.POST("/seller/pending/confirm", h.ConfirmSellerPending)
	g.POST("/seller/pending/reject", h.RejectSellerPending)

	// Seller - Confirmed
	g.GET("/seller/confirmed", h.ListSellerConfirmed)
	g.GET("/seller/confirmed/:id", h.GetSellerOrder)

	// Seller - Refund
	g.POST("/seller/refund/confirm", h.ConfirmSellerRefund)

	// Dispute
	g.GET("/disputes", h.ListRefundDisputes)
	g.GET("/disputes/:disputeID", h.GetRefundDispute)
	g.POST("/refunds/:refundID/disputes", h.CreateRefundDispute, rlDispute)
	g.GET("/refunds/:refundID/disputes", h.ListRefundDisputesByRefund)

	// Payment webhooks — register OnResult then mount routes
	onResult := func(ctx context.Context, result payment.WebhookResult) error {
		return biz.ConfirmPayment(ctx, orderbiz.ConfirmPaymentParams{
			RefID:  result.RefID,
			Status: result.Status,
		})
	}
	for _, client := range handler.PaymentClients() {
		client.OnResult(onResult)
		client.InitializeWebhook(e)
	}

	// Transport webhooks — register OnResult then mount routes
	onTransportResult := func(ctx context.Context, result transport.WebhookResult) error {
		// TODO: implement transport ID lookup — GHTK sends label ID, not UUID.
		// Need a GetTransportByTrackingID query to map provider ID → transport UUID.
		transportID, err := uuid.Parse(result.TransportID)
		if err != nil {
			slog.Warn("transport webhook: cannot parse transport ID as UUID, provider lookup needed",
				slog.String("transport_id", result.TransportID))
			return nil
		}
		return biz.UpdateTransportStatus(ctx, orderbiz.UpdateTransportStatusParams{
			TransportID: transportID,
			Status:      orderdb.OrderTransportStatus(result.Status),
			Data:        result.Data,
		})
	}
	for _, client := range handler.TransportClients() {
		client.OnResult(onTransportResult)
		client.InitializeWebhook(e)
	}

	return h
}

// --- Buyer Order ---

type GetBuyerOrderRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetBuyerOrder(c echo.Context) error {
	var req GetBuyerOrderRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if _, err := authclaims.GetClaims(c.Request()); err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.GetBuyerOrder(c.Request().Context(), req.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type GetSellerOrderRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetSellerOrder(c echo.Context) error {
	var req GetSellerOrderRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if _, err := authclaims.GetClaims(c.Request()); err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}
	result, err := h.biz.GetSellerOrder(c.Request().Context(), req.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListBuyerConfirmedRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerConfirmed(c echo.Context) error {
	var req ListBuyerConfirmedRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.ListBuyerConfirmed(c.Request().Context(), orderbiz.ListBuyerConfirmedParams{
		BuyerID:          claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ListSellerConfirmedRequest struct {
	Search null.String `query:"search"`
	sharedmodel.PaginationParams
}

func (h *Handler) ListSellerConfirmed(c echo.Context) error {
	var req ListSellerConfirmedRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.ListSellerConfirmed(c.Request().Context(), orderbiz.ListSellerConfirmedParams{
		SellerID:         claims.Account.ID,
		Search:           req.Search,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

// --- Buyer Checkout ---

type BuyerCheckoutRequest struct {
	BuyNow        bool                  `json:"buy_now" validate:"omitempty"`
	Address       string                `json:"address" validate:"required,min=1,max=500"`
	PaymentOption string                `json:"payment_option" validate:"max=100"`
	UseWallet     bool                  `json:"use_wallet"`
	Items         []CheckoutItemRequest `json:"items"   validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
	SkuID           uuid.UUID `json:"sku_id"           validate:"required"`
	Quantity        int64     `json:"quantity"          validate:"required,gt=0"`
	TransportOption string    `json:"transport_option"  validate:"required,min=1,max=100"`
	Note            string    `json:"note"              validate:"max=500"`
}

func (h *Handler) BuyerCheckout(c echo.Context) error {
	var req BuyerCheckoutRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	items := make([]orderbiz.CheckoutItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, orderbiz.CheckoutItem{
			SkuID:           item.SkuID,
			Quantity:        item.Quantity,
			TransportOption: item.TransportOption,
			Note:            item.Note,
		})
	}

	result, err := h.biz.BuyerCheckout(c.Request().Context(), orderbiz.BuyerCheckoutParams{
		Account:       claims.Account,
		BuyNow:        req.BuyNow,
		Address:       req.Address,
		PaymentOption: req.PaymentOption,
		UseWallet:     req.UseWallet,
		Items:         items,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

// --- Buyer Pending Items ---

type ListBuyerPendingItemsRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerPendingItems(c echo.Context) error {
	var req ListBuyerPendingItemsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.ListBuyerPendingItems(c.Request().Context(), orderbiz.ListBuyerPendingItemsParams{
		AccountID:        claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) CancelBuyerPending(c echo.Context) error {
	idStr := c.Param("id")
	itemID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	if err := h.biz.CancelBuyerPending(c.Request().Context(), orderbiz.CancelBuyerPendingParams{
		AccountID: claims.Account.ID,
		ItemID:    itemID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Item cancelled successfully")
}


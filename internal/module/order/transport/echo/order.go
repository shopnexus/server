package orderecho

import (
	"log/slog"
	"net/http"
	"strconv"

	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Handler handles HTTP requests for the order module.
type Handler struct {
	biz orderbiz.OrderBiz
}

// NewHandler registers order module routes and returns the handler.
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz) *Handler {
	h := &Handler{biz: biz}
	g := e.Group("/api/v1/order")

	// Cart (unchanged)
	g.GET("/cart", h.GetCart)
	g.POST("/cart", h.UpdateCart)
	g.DELETE("/cart", h.ClearCart)

	// Checkout
	g.POST("/checkout", h.Checkout)
	g.GET("/checkout/items", h.ListPendingItems)
	g.DELETE("/checkout/items/:id", h.CancelPendingItem)

	// Incoming (seller)
	g.GET("/incoming", h.ListIncomingItems)
	g.POST("/incoming/confirm", h.ConfirmItems)
	g.POST("/incoming/reject", h.RejectItems)

	// Orders (literal paths before parameterized!)
	g.GET("", h.ListOrders)
	g.GET("/seller", h.ListSellerOrders)
	g.GET("/:id", h.GetOrder)

	// Payment
	g.POST("/pay", h.PayOrders)

	// IPN (no auth)
	e.GET("/api/v1/order/ipn", h.VnpayVerifyIPN)

	// Refund (unchanged routes)
	refundApi := g.Group("/refund")
	refundApi.GET("", h.ListRefunds)
	refundApi.POST("", h.CreateRefund)
	refundApi.PATCH("", h.UpdateRefund)
	refundApi.DELETE("", h.CancelRefund)
	refundApi.POST("/confirm", h.ConfirmRefund)

	return h
}

type GetOrderRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetOrder(c echo.Context) error {
	var req GetOrderRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if _, err := authclaims.GetClaims(c.Request()); err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.GetOrder(c.Request().Context(), req.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListOrdersRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListOrders(c echo.Context) error {
	var req ListOrdersRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListOrders(c.Request().Context(), orderbiz.ListOrdersParams{
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ListSellerOrdersRequest struct {
	sharedmodel.PaginationParams
	PaymentStatus []orderdb.OrderStatus `query:"payment_status"`
	OrderStatus   []orderdb.OrderStatus `query:"order_status"`
}

func (h *Handler) ListSellerOrders(c echo.Context) error {
	var req ListSellerOrdersRequest
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

	result, err := h.biz.ListSellerOrders(c.Request().Context(), orderbiz.ListSellerOrdersParams{
		SellerID:         claims.Account.ID,
		PaymentStatus:    req.PaymentStatus,
		OrderStatus:      req.OrderStatus,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

// --- Checkout ---

type CheckoutRequest struct {
	BuyNow bool                  `json:"buy_now" validate:"omitempty"`
	Items  []CheckoutItemRequest `json:"items" validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
	SkuID    uuid.UUID `json:"sku_id" validate:"required"`
	Quantity int64     `json:"quantity" validate:"required,gt=0"`
	Address  string    `json:"address" validate:"required,min=1,max=500"`
	Note     string    `json:"note" validate:"max=500"`
}

func (h *Handler) Checkout(c echo.Context) error {
	var req CheckoutRequest
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
			SkuID:    item.SkuID,
			Quantity: item.Quantity,
			Address:  item.Address,
			Note:     item.Note,
		})
	}

	result, err := h.biz.Checkout(c.Request().Context(), orderbiz.CheckoutParams{
		Account: claims.Account,
		BuyNow:  req.BuyNow,
		Items:   items,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

// --- Pending Items ---

type ListPendingItemsRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListPendingItems(c echo.Context) error {
	var req ListPendingItemsRequest
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

	result, err := h.biz.ListPendingItems(c.Request().Context(), orderbiz.ListPendingItemsParams{
		AccountID:        claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) CancelPendingItem(c echo.Context) error {
	idStr := c.Param("id")
	itemID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	if err := h.biz.CancelPendingItem(c.Request().Context(), orderbiz.CancelPendingItemParams{
		AccountID: claims.Account.ID,
		ItemID:    itemID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Item cancelled successfully")
}

// --- Payment ---

type PayOrdersRequest struct {
	OrderIDs      []uuid.UUID `json:"order_ids" validate:"required,min=1"`
	PaymentOption string      `json:"payment_option" validate:"required,min=1,max=100"`
}

func (h *Handler) PayOrders(c echo.Context) error {
	var req PayOrdersRequest
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

	result, err := h.biz.PayOrders(c.Request().Context(), orderbiz.PayOrdersParams{
		Account:       claims.Account,
		OrderIDs:      req.OrderIDs,
		PaymentOption: req.PaymentOption,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

// --- Cancel Order ---

type CancelOrderRequest struct {
	OrderID uuid.UUID `json:"order_id" validate:"required"`
}

func (h *Handler) CancelOrder(c echo.Context) error {
	var req CancelOrderRequest
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

	if err := h.biz.CancelOrder(c.Request().Context(), orderbiz.CancelOrderParams{
		Account: claims.Account,
		OrderID: req.OrderID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Order cancelled successfully")
}

// --- IPN ---

func (h *Handler) VnpayVerifyIPN(c echo.Context) error {
	if err := c.Request().ParseForm(); err != nil {
		slog.Error("VnpayVerifyIPN parse form error", slog.Any("error", err))
		return c.NoContent(http.StatusBadRequest)
	}

	query := make(map[string]any)
	for key, values := range c.Request().Form {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}

	if err := h.biz.VerifyPayment(c.Request().Context(), orderbiz.VerifyPaymentParams{
		PaymentGateway: "vnpay_qr",
		Data:           query,
	}); err != nil {
		slog.Error("VnpayVerifyIPN verify error", slog.Any("error", err))
		return c.NoContent(http.StatusBadRequest)
	}

	return c.NoContent(http.StatusOK)
}

package orderecho

import (
	"context"
	"net/http"
	"strconv"

	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	"shopnexus-server/internal/provider/payment"
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
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz, handler *orderbiz.OrderHandler) *Handler {
	h := &Handler{biz: biz}
	g := e.Group("/api/v1/order")

	// Cart (unchanged)
	g.GET("/cart", h.GetCart)
	g.POST("/cart", h.UpdateCart)
	g.DELETE("/cart", h.ClearCart)

	// Buyer - Pending
	g.POST("/buyer/checkout", h.BuyerCheckout)
	g.GET("/buyer/pending", h.ListBuyerPending)
	g.DELETE("/buyer/pending/:id", h.CancelBuyerPending)

	// Buyer - Confirmed
	g.GET("/buyer/confirmed", h.ListBuyerConfirmed)
	g.GET("/buyer/confirmed/:id", h.GetBuyerOrder)
	g.DELETE("/buyer/confirmed/:id", h.CancelBuyerOrder)
	g.POST("/buyer/pay", h.PayBuyerOrders)

	// Buyer - Refund
	buyerRefund := g.Group("/buyer/refund")
	buyerRefund.GET("", h.ListBuyerRefunds)
	buyerRefund.POST("", h.CreateBuyerRefund)
	buyerRefund.PATCH("", h.UpdateBuyerRefund)
	buyerRefund.DELETE("", h.CancelBuyerRefund)

	// Seller - Pending
	g.GET("/seller/pending", h.ListSellerPending)
	g.POST("/seller/pending/confirm", h.ConfirmSellerPending)
	g.POST("/seller/pending/reject", h.RejectSellerPending)

	// Seller - Confirmed
	g.GET("/seller/confirmed", h.ListSellerConfirmed)
	g.GET("/seller/confirmed/:id", h.GetSellerOrder)

	// Seller - Refund
	g.POST("/seller/refund/confirm", h.ConfirmSellerRefund)

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
	Status []orderdb.OrderStatus `query:"status" validate:"omitempty"`
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

	result, err := h.biz.ListBuyerConfirmed(c.Request().Context(), orderbiz.ListBuyerConfirmedParams{
		PaginationParams: req.PaginationParams.Constrain(),
		Status:           req.Status,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ListSellerConfirmedRequest struct {
	Search        null.String           `query:"search"`
	PaymentStatus []orderdb.OrderStatus `query:"payment_status"`
	OrderStatus   []orderdb.OrderStatus `query:"order_status"`
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
		PaymentStatus:    req.PaymentStatus,
		OrderStatus:      req.OrderStatus,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

// --- Buyer Checkout ---

type BuyerCheckoutRequest struct {
	BuyNow bool                  `json:"buy_now" validate:"omitempty"`
	Items  []CheckoutItemRequest `json:"items" validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
	SkuID    uuid.UUID `json:"sku_id" validate:"required"`
	Quantity int64     `json:"quantity" validate:"required,gt=0"`
	Address  string    `json:"address" validate:"required,min=1,max=500"`
	Note     string    `json:"note" validate:"max=500"`
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
			SkuID:    item.SkuID,
			Quantity: item.Quantity,
			Address:  item.Address,
			Note:     item.Note,
		})
	}

	result, err := h.biz.BuyerCheckout(c.Request().Context(), orderbiz.BuyerCheckoutParams{
		Account: claims.Account,
		BuyNow:  req.BuyNow,
		Items:   items,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

// --- Buyer Pending Items ---

type ListBuyerPendingRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerPending(c echo.Context) error {
	var req ListBuyerPendingRequest
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

	result, err := h.biz.ListBuyerPending(c.Request().Context(), orderbiz.ListBuyerPendingParams{
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

// --- Payment ---

type PayBuyerOrdersRequest struct {
	OrderIDs      []uuid.UUID `json:"order_ids" validate:"required,min=1"`
	PaymentOption string      `json:"payment_option" validate:"required,min=1,max=100"`
}

func (h *Handler) PayBuyerOrders(c echo.Context) error {
	var req PayBuyerOrdersRequest
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

	result, err := h.biz.PayBuyerOrders(c.Request().Context(), orderbiz.PayBuyerOrdersParams{
		Account:       claims.Account,
		OrderIDs:      req.OrderIDs,
		PaymentOption: req.PaymentOption,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

// --- Cancel Buyer Order ---

type CancelBuyerOrderRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) CancelBuyerOrder(c echo.Context) error {
	var req CancelBuyerOrderRequest
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

	if err := h.biz.CancelBuyerOrder(c.Request().Context(), orderbiz.CancelBuyerOrderParams{
		Account: claims.Account,
		OrderID: req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Order cancelled successfully")
}

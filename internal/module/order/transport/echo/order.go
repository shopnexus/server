package orderecho

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/ratelimit"
	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	"shopnexus-server/internal/provider/transport"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
	"github.com/restatedev/sdk-go/ingress"
)

// Handler handles HTTP requests for the order module.
type Handler struct {
	biz     orderbiz.OrderBiz
	ingress *ingress.Client
}

// NewHandler registers order module routes and returns the handler.
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz, handler *orderbiz.OrderHandler, rl *ratelimit.Factory, cfg *config.Config) *Handler {
	h := &Handler{
		biz:     biz,
		ingress: ingress.NewClient(cfg.Restate.IngressAddress),
	}
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
	g.POST("/buyer/checkout/:sessionID/cancel", h.CancelBuyerCheckout)
	g.GET("/buyer/pending", h.ListBuyerPendingItems)
	g.DELETE("/buyer/pending/:id", h.CancelBuyerPending)

	// Buyer - Confirmed
	g.GET("/buyer/confirmed", h.ListBuyerConfirmed)
	g.GET("/buyer/confirmed/:id", h.GetBuyerOrder)

	// Buyer - Refund
	buyerRefund := g.Group("/buyer/refund")
	buyerRefund.GET("", h.ListBuyerRefunds)
	buyerRefund.POST("", h.CreateBuyerRefund, rlRefund)

	// Seller - Pending
	// TODO: add casbin role middleware for /seller/* routes
	g.GET("/seller/pending", h.ListSellerPendingItems)
	g.POST("/seller/pending/confirm", h.ConfirmSellerPending)
	g.POST("/seller/pending/confirm/:sessionID/cancel", h.CancelConfirmSellerPending)
	g.POST("/seller/pending/reject", h.RejectSellerPending)

	// Seller - Confirmed
	g.GET("/seller/confirmed", h.ListSellerConfirmed)
	g.GET("/seller/confirmed/:id", h.GetSellerOrder)

	// Seller - Refund
	g.GET("/seller/refund", h.ListSellerRefunds)

	// Refund stage actions
	refund := g.Group("/refunds/:id")
	refund.POST("/accept", h.AcceptRefundStage1, rlRefund)
	refund.POST("/approve", h.ApproveRefundStage2, rlRefund)
	refund.POST("/reject", h.RejectRefund, rlRefund)

	// Dispute
	g.GET("/disputes", h.ListRefundDisputes)
	g.GET("/disputes/:disputeID", h.GetRefundDispute)
	g.POST("/refunds/:refundID/disputes", h.CreateRefundDispute, rlDispute)
	g.GET("/refunds/:refundID/disputes", h.ListRefundDisputesByRefund)

	// registered tracks webhook idempotency keys returned by WireWebhooks so
	// providers that share an endpoint (e.g., GHTK express/standard/economy)
	// only mount their route once.
	registered := make(map[string]struct{})

	opts, err := handler.GetOptions(context.Background(), orderbiz.GetOptionsParams{Type: sharedmodel.OptionTypePayment})
	if err != nil {
		panic(fmt.Errorf("load payment options: %w", err))
	}
	for _, opt := range opts {
		client := newPaymentClient(opt)
		if client == nil {
			continue
		}
		if key := client.WireWebhooks(e, h.biz.OnPaymentResult, registered); key != "" {
			registered[key] = struct{}{}
		}
	}

	// Transport webhooks — use OrderStatus (not OrderTransportStatus)
	onTransportResult := func(ctx context.Context, result transport.WebhookResult) error {
		data, err := json.Marshal(result.Data)
		if err != nil {
			return fmt.Errorf("marshal transport webhook data: %w", err)
		}
		return biz.OnTransportResult(ctx, orderbiz.OnTransportResultParams{
			TrackingID: result.TransportID,
			Status:     orderdb.OrderStatus(result.Status),
			Data:       data,
		})
	}
	transportOpts, err := handler.GetOptions(context.Background(), orderbiz.GetOptionsParams{Type: sharedmodel.OptionTypeTransport})
	if err != nil {
		panic(fmt.Errorf("load transport options: %w", err))
	}
	for _, opt := range transportOpts {
		client := newTransportClient(opt)
		if client == nil {
			continue
		}
		if key := client.WireWebhooks(e, onTransportResult, registered); key != "" {
			registered[key] = struct{}{}
		}
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
	WalletID      *uuid.UUID            `json:"wallet_id,omitempty"`
	Items         []CheckoutItemRequest `json:"items"   validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
	SkuID           uuid.UUID `json:"sku_id"           validate:"required"`
	Quantity        int64     `json:"quantity"          validate:"required,gt=0"`
	TransportOption string    `json:"transport_option"  validate:"required,min=1,max=100"`
	Note            string    `json:"note"              validate:"max=500"`
}

// BuyerCheckoutResponse is the sync envelope returned by /buyer/checkout. The
// session ID doubles as the workflow ID and the payment-gateway RefID, so
// clients can poll/cancel against the same key. PaymentURL is empty for
// wallet-only checkouts (no gateway redirect needed).
type BuyerCheckoutResponse struct {
	CheckoutSessionID string `json:"checkout_session_id"`
	PaymentURL        string `json:"payment_url"`
}

// BuyerCheckout submits a CheckoutWorkflow and synchronously attaches to its
// shared WaitPaymentURL handler so the response carries the gateway redirect
// (or empty for wallet-only). The workflow continues running asynchronously
// after this handler returns; the buyer can later cancel via
// /buyer/checkout/:sessionID/cancel which signals CancelCheckout.
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

	workflowID := uuid.NewString()
	input := orderbiz.CheckoutWorkflowInput{
		Account:       claims.Account,
		Items:         items,
		Address:       req.Address,
		BuyNow:        req.BuyNow,
		UseWallet:     req.UseWallet,
		WalletID:      req.WalletID,
		PaymentOption: req.PaymentOption,
	}

	ctx := c.Request().Context()

	// Submit Run as fire-and-forget — Restate journal owns the lifecycle from
	// here. We don't wait for Run() to return; instead we attach to the shared
	// WaitPaymentURL promise which Run() resolves once the gateway URL is known.
	if _, err := ingress.Workflow[orderbiz.CheckoutWorkflowInput, struct{}](
		h.ingress, "CheckoutWorkflow", workflowID, "Run",
	).Send(ctx, input); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	url, err := ingress.Workflow[struct{}, string](
		h.ingress, "CheckoutWorkflow", workflowID, "WaitPaymentURL",
	).Request(ctx, struct{}{})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, BuyerCheckoutResponse{
		CheckoutSessionID: workflowID,
		PaymentURL:        url,
	})
}

// CancelBuyerCheckout signals CheckoutWorkflow.CancelCheckout for the given
// session, which resolves the workflow's payment_event promise with
// kind="cancelled" so Run() unwinds via its saga compensators.
func (h *Handler) CancelBuyerCheckout(c echo.Context) error {
	sessionID := c.Param("sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, fmt.Errorf("invalid session id: %w", err))
	}

	if _, err := authclaims.GetClaims(c.Request()); err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	if _, err := ingress.Workflow[struct{}, struct{}](
		h.ingress, "CheckoutWorkflow", sessionID, "CancelCheckout",
	).Send(c.Request().Context(), struct{}{}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Checkout cancelled")
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

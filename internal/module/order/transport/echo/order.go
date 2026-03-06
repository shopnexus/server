package orderecho

import (
	"encoding/json"
	"log/slog"
	"net/http"

	orderbiz "shopnexus-remastered/internal/module/order/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

type Handler struct {
	biz *orderbiz.OrderBiz
}

func NewHandler(e *echo.Echo, biz *orderbiz.OrderBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/order")

	api.GET("", h.ListOrders)
	api.GET("/:id", h.GetOrder)
	api.GET("/cart", h.GetCart)
	api.GET("/cart-checkout", h.ListCheckoutCart)
	api.POST("/checkout", h.Checkout)
	api.POST("/confirm", h.ConfirmOrder)
	api.POST("/quote", h.QuoteOrder)
	api.GET("/vendor", h.ListVendorOrder)

	// Cart endpoints
	cartApi := api.Group("/cart")
	cartApi.GET("", h.GetCart)
	cartApi.POST("", h.UpdateCart)
	cartApi.DELETE("", h.ClearCart)

	refundApi := api.Group("/refund")
	refundApi.GET("", h.ListRefunds)
	refundApi.POST("", h.CreateRefund)
	refundApi.PATCH("", h.UpdateRefund)
	refundApi.DELETE("", h.CancelRefund)
	refundApi.POST("/confirm", h.ConfirmRefund)

	// Verify vnpay ipn
	//api.GET("/vnpay/ipn", echo.WrapHandler(h.biz.))
	api.GET("/ipn", h.VnpayVerifyIPN)

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
	commonmodel.PaginationParams
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

type CheckoutRequest struct {
	Address       string                `json:"address" validate:"required"`
	BuyNow        bool                  `json:"buy_now" validate:"omitempty"`
	PaymentOption string                `json:"payment_option" validate:"required,min=1,max=100"`
	Items         []CheckoutItemRequest `json:"items" validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
	SkuID          uuid.UUID       `json:"sku_id" validate:"required"`
	Quantity       int64           `json:"quantity" validate:"required,gt=0"`
	Note           string          `json:"note" validate:"max=500"`
	ShipmentOption string          `json:"shipment_option" validate:"required,min=1,max=100"`
	PromotionCodes []string        `json:"promotion_codes" validate:"dive"`
	Data           json.RawMessage `json:"data" validate:"omitempty"`
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

	result, err := h.biz.Checkout(c.Request().Context(), orderbiz.CheckoutParams{
		Account:       claims.Account,
		Address:       req.Address,
		BuyNow:        req.BuyNow,
		PaymentOption: req.PaymentOption,
		Items: lo.Map(req.Items, func(item CheckoutItemRequest, _ int) orderbiz.CheckoutItem {
			return orderbiz.CheckoutItem{
				SkuID:          item.SkuID,
				Quantity:       item.Quantity,
				PromotionCodes: item.PromotionCodes,
				ShipmentOption: item.ShipmentOption,
				Note:           item.Note,
				Data:           item.Data,
			}
		}),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type QuoteRequest struct {
	Address string                `json:"address" validate:"required"`
	Items   []CheckoutItemRequest `json:"items" validate:"required,min=1,dive"`
}

func (h *Handler) QuoteOrder(c echo.Context) error {
	var req QuoteRequest
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

	result, err := h.biz.QuoteOrder(c.Request().Context(), orderbiz.QuoteOrderParams{
		Account: claims.Account,
		Address: req.Address,
		Items: lo.Map(req.Items, func(item CheckoutItemRequest, _ int) orderbiz.CheckoutItem {
			return orderbiz.CheckoutItem{
				SkuID:          item.SkuID,
				Quantity:       item.Quantity,
				PromotionCodes: item.PromotionCodes,
				ShipmentOption: item.ShipmentOption,
				Note:           item.Note,
				Data:           item.Data,
			}
		}),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

func (h *Handler) VnpayVerifyIPN(c echo.Context) error {
	var query map[string]any

	//if err := c.Bind(&query); err != nil {
	//	logger.Log.Sugar().Errorln("VnpayVerifyIPN bind error:", err)
	//	return c.NoContent(http.StatusBadRequest)
	//}
	if err := c.Request().ParseForm(); err != nil {
		slog.Error("VnpayVerifyIPN parse form error", slog.Any("error", err))
		return c.NoContent(http.StatusBadRequest)
	}

	query = make(map[string]any)
	for key, values := range c.Request().Form {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}

	// Verify the checksum hash
	if err := h.biz.VerifyPayment(c.Request().Context(), orderbiz.VerifyPaymentParams{
		PaymentGateway: "vnpay_qr",
		Data:           query,
	}); err != nil {
		slog.Error("VnpayVerifyIPN verify error", slog.Any("error", err))
		return c.NoContent(http.StatusBadRequest)
	}

	return c.NoContent(http.StatusOK)
}

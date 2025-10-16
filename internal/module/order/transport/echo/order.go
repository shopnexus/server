package orderecho

import (
	"net/http"

	"shopnexus-remastered/internal/logger"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *orderbiz.OrderBiz
}

func NewHandler(e *echo.Echo, biz *orderbiz.OrderBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/order")

	api.GET("", h.ListOrders)
	api.GET("/:id", h.GetOrder)
	api.POST("/checkout", h.Checkout)
	api.POST("/confirm", h.ConfirmOrder)

	refundApi := api.Group("/refund")
	refundApi.GET("", h.ListRefunds)
	refundApi.POST("", h.CreateRefund)
	refundApi.PATCH("", h.UpdateRefund)
	refundApi.DELETE("", h.CancelRefund)

	// Verify vnpay ipn
	//api.GET("/vnpay/ipn", echo.WrapHandler(h.biz.))
	api.GET("/ipn", h.VnpayVerifyIPN)

	return h
}

type GetOrderRequest struct {
	ID int64 `param:"id" validate:"required"`
}

func (h *Handler) GetOrder(c echo.Context) error {
	var req GetOrderRequest
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

	result, err := h.biz.GetOrder(c.Request().Context(), orderbiz.GetOrderParams{
		Account: claims.Account,
		OrderID: req.ID,
	})
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

	//claims, err := authclaims.GetClaims(c.Request())
	//if err != nil {
	//	return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	//}

	result, err := h.biz.ListOrders(c.Request().Context(), orderbiz.ListOrdersParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type CheckoutRequest struct {
	Address       string        `json:"address" validate:"required"`
	PaymentOption string        `json:"payment_option" validate:"required,min=1,max=100"`
	Skus          []CheckoutSku `json:"skus" validate:"required,dive"`
}

type CheckoutSku struct {
	SkuID          int64   `json:"sku_id" validate:"required,gt=0"`
	PromotionIDs   []int64 `json:"promotion_ids" validate:"dive,gt=0"`
	ShipmentOption string  `json:"shipment_option" validate:"required,min=1,max=100"`
	Note           string  `json:"note" validate:"max=500"` // Note for this item, e.g. "Please gift wrap this item"
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

	result, err := h.biz.CreateOrder(c.Request().Context(), orderbiz.CreateOrderParams{
		Account:       claims.Account,
		Address:       req.Address,
		PaymentOption: req.PaymentOption,
		Skus: slice.Map(req.Skus, func(s CheckoutSku) orderbiz.OrderSku {
			return orderbiz.OrderSku{
				SkuID:          s.SkuID,
				PromotionIDs:   s.PromotionIDs,
				ShipmentOption: s.ShipmentOption,
				Note:           s.Note,
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
		logger.Log.Sugar().Errorln("VnpayVerifyIPN parse form error:", err)
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
		PaymentGateway: "vnpay_card", // or "vnpay_banktransfer"
		Data:           query,
	}); err != nil {
		logger.Log.Sugar().Errorln("VnpayVerifyIPN verify error:", err)
		return c.NoContent(http.StatusBadRequest)
	}

	return c.NoContent(http.StatusOK)
}

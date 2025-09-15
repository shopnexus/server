package orderecho

import (
	"net/http"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/logger"
	authbiz "shopnexus-remastered/internal/module/auth/biz"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *orderbiz.OrderBiz
}

func NewHandler(e *echo.Echo, biz *orderbiz.OrderBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/order")

	api.POST("/checkout", h.Checkout)

	// Verify vnpay ipn
	//api.GET("/vnpay/ipn", echo.WrapHandler(h.biz.))
	api.GET("/ipn", h.VnpayVerifyIPN)

	return h
}

type CheckoutRequest struct {
	Address     string                `json:"address" validate:"required"`
	OrderMethod db.OrderPaymentMethod `json:"order_method" validate:"required"`
	SkuIDs      []int64               `json:"sku_ids" validate:"required"`
}

func (h *Handler) Checkout(c echo.Context) error {
	var req CheckoutRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.CreateOrder(c.Request().Context(), orderbiz.CreateOrderParams{
		Account:     claims.Account,
		Address:     req.Address,
		OrderMethod: req.OrderMethod,
		SkuIDs:      req.SkuIDs,
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
		Method: db.OrderPaymentMethodEWallet,
		Query:  query,
	}); err != nil {
		logger.Log.Sugar().Errorln("VnpayVerifyIPN verify error:", err)
		return c.NoContent(http.StatusBadRequest)
	}

	return c.NoContent(http.StatusOK)
}

package orderecho

import (
	"fmt"
	"net/http"
	"shopnexus-remastered/internal/db"
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
	fmt.Print("asdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkkasdasdkkkkkkkkkkkk")

	return h
}

type CheckoutRequest struct {
	Address     string                `json:"address" validate:"required"`
	OrderMethod db.OrderPaymentMethod `json:"order_method" validate:"required,oneof='ewallet' 'cod'"`
	SkuIDs      []int64               `json:"sku_ids" validate:"required,dive,gt=0"`
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

	claims.Account.ID = 14

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

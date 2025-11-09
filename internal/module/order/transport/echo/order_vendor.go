package orderecho

import (
	"net/http"

	"shopnexus-remastered/internal/infras/shipment"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	"shopnexus-remastered/internal/module/shared/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListVendorOrderRequest struct {
	commonmodel.PaginationParams
}

func (h *Handler) ListVendorOrder(c echo.Context) error {
	var req ListVendorOrderRequest
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

	result, err := h.biz.ListVendorOrder(c.Request().Context(), orderbiz.ListVendorOrderParams{
		Account:          claims.Account,
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ConfirmOrderRequest struct {
	OrderItemID int64 `json:"order_item_id" validate:"required,min=1"` // Confirmed SKU

	FromAddress null.String             `json:"from_address" validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	Package     shipment.PackageDetails `json:"package" validate:"required"`                   // JSON object with weight and dimensions
}

func (h *Handler) ConfirmOrder(c echo.Context) error {
	var req ConfirmOrderRequest
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

	if err = h.biz.ConfirmOrder(c.Request().Context(), orderbiz.ConfirmOrderParams{
		Account:     claims.Account,
		OrderItemID: req.OrderItemID,
		FromAddress: req.FromAddress,
		Package:     req.Package,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "order confirmed successfully")
}

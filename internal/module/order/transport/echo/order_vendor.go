package orderecho

import (
	"encoding/json"
	"net/http"

	orderbiz "shopnexus-server/internal/module/order/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
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
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ConfirmOrderRequest struct {
	OrderID uuid.UUID `json:"order_id" validate:"required"`

	FromAddress null.String     `json:"from_address" validate:"omitnil,min=5,max=500"`
	Package     json.RawMessage `json:"package" validate:"omitempty"`
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

	if err := h.biz.ConfirmOrder(c.Request().Context(), orderbiz.ConfirmOrderParams{
		Account:     claims.Account,
		OrderID:     req.OrderID,
		FromAddress: req.FromAddress,
		Package:     req.Package,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "order confirmed successfully")
}

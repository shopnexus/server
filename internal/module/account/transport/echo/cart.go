package accountecho

import (
	"net/http"

	"github.com/guregu/null/v6"

	accountbiz "shopnexus-remastered/internal/module/account/biz"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type GetCartRequest struct {
}

func (h *Handler) GetCart(c echo.Context) error {
	var req GetCartRequest
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

	result, err := h.biz.GetCart(c.Request().Context(), accountbiz.GetCartParams{
		AccountID: claims.Account.ID,
	})

	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateCartRequest struct {
	SkuID         int64      `json:"sku_id" validate:"required"`
	Quantity      null.Int64 `json:"quantity" validate:"omitnil"`
	DeltaQuantity null.Int64 `json:"delta_quantity" validate:"omitnil"`
}

func (h *Handler) UpdateCart(c echo.Context) error {
	var req UpdateCartRequest
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

	if err := h.biz.UpdateCart(c.Request().Context(), accountbiz.UpdateCartParams{
		Account:       claims.Account,
		SkuID:         req.SkuID,
		Quantity:      req.Quantity,
		DeltaQuantity: req.DeltaQuantity,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Update cart successfully")
}

type ClearCartRequest struct {
}

func (h *Handler) ClearCart(c echo.Context) error {
	var req ClearCartRequest
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

	if err := h.biz.ClearCart(c.Request().Context(), accountbiz.ClearCartParams{
		Account: claims.Account,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Clear cart successfully")
}

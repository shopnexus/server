package orderecho

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"

	orderbiz "shopnexus-server/internal/module/order/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	"shopnexus-server/internal/shared/response"

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

	result, err := h.biz.GetCart(c.Request().Context(), orderbiz.GetCartParams{
		AccountID: claims.Account.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateCartRequest struct {
	SkuID         uuid.UUID  `json:"sku_id" validate:"required"`
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

	if err := h.biz.UpdateCart(c.Request().Context(), orderbiz.UpdateCartParams{
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

	if err := h.biz.ClearCart(c.Request().Context(), orderbiz.ClearCartParams{
		Account: claims.Account,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Clear cart successfully")
}

type ListCheckoutCartRequest struct {
	SkuIDs         []uuid.UUID   `query:"sku_ids" validate:"omitempty,dive"`                  // Select items in cart to checkout
	BuyNowSkuID    uuid.NullUUID `query:"buy_now_sku_id" validate:"omitnil"`                  // Instant checkout
	BuyNowQuantity null.Int64    `query:"buy_now_quantity" validate:"omitnil,min=1,max=1000"` // Instant checkout quantity
}

func (h *Handler) ListCheckoutCart(c echo.Context) error {
	var req ListCheckoutCartRequest
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

	result, err := h.biz.ListCheckoutCart(c.Request().Context(), orderbiz.ListCheckoutCartParams{
		Account:        claims.Account,
		SkuIDs:         req.SkuIDs,
		BuyNowSkuID:    req.BuyNowSkuID,
		BuyNowQuantity: req.BuyNowQuantity,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

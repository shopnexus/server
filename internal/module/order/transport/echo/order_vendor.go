package orderecho

import (
	"net/http"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ConfirmOrderRequest struct {
	SkuID int64 `json:"sku_id" validate:"required,min=1"` // Confirmed SKU

	FromAddress null.String `json:"from_address" validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	WeightGrams int32       `json:"weight_grams" validate:"required,min=1"`        // Revalidated weight, dimensions
	LengthCM    int32       `json:"length_cm" validate:"required,min=1"`
	WidthCM     int32       `json:"width_cm" validate:"required,min=1"`
	HeightCM    int32       `json:"height_cm" validate:"required,min=1"`
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
		SkuID:       req.SkuID,
		FromAddress: req.FromAddress,
		WeightGrams: req.WeightGrams,
		LengthCM:    req.LengthCM,
		WidthCM:     req.WidthCM,
		HeightCM:    req.HeightCM,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "order confirmed successfully")
}

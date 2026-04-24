package accountecho

import (
	"net/http"

	authclaims "shopnexus-server/internal/shared/claims"
	"shopnexus-server/internal/shared/response"

	"github.com/labstack/echo/v4"
)

func (h *Handler) GetWalletBalance(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	balance, err := h.biz.GetWalletBalance(c.Request().Context(), claims.Account.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]int64{
		"balance": balance,
	})
}

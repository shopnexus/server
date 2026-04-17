package accountecho

import (
	"net/http"

	accountbiz "shopnexus-server/internal/module/account/biz"
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

type ListWalletTransactionsRequest struct {
	Limit  int64 `query:"limit" validate:"omitempty,min=1,max=100"`
	Offset int64 `query:"offset" validate:"omitempty,min=0"`
}

func (h *Handler) ListWalletTransactions(c echo.Context) error {
	var req ListWalletTransactionsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if req.Limit == 0 {
		req.Limit = 20
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	transactions, err := h.biz.ListWalletTransactions(c.Request().Context(), accountbiz.ListWalletTransactionsParams{
		AccountID: claims.Account.ID,
		Limit:     req.Limit,
		Offset:    req.Offset,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, transactions)
}

package accountecho

import (
	"net/http"

	accountbiz "shopnexus-server/internal/module/account/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
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
	sharedmodel.PaginationParams
}

func (h *Handler) ListWalletTransactions(c echo.Context) error {
	var req ListWalletTransactionsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	transactions, err := h.biz.ListWalletTransactions(c.Request().Context(), accountbiz.ListWalletTransactionsParams{
		PaginationParams: req.PaginationParams,
		AccountID:        claims.Account.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, transactions)
}

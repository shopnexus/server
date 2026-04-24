package accountecho

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	accountbiz "shopnexus-server/internal/module/account/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	"shopnexus-server/internal/shared/response"
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

type CreateWalletRequest struct {
	Option string          `json:"option" validate:"required,max=100"`
	Label  string          `json:"label" validate:"required,max=100"`
	Data   json.RawMessage `json:"data"`
}

func (h *Handler) CreateWallet(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	var req CreateWalletRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	wallet, err := h.biz.CreateWallet(c.Request().Context(), accountbiz.CreateWalletParams{
		AccountID: claims.Account.ID,
		Option:    req.Option,
		Label:     req.Label,
		Data:      req.Data,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, wallet)
}

func (h *Handler) ListWallets(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	wallets, err := h.biz.ListWallets(c.Request().Context(), accountbiz.ListWalletsParams{
		AccountID: claims.Account.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, wallets)
}

func (h *Handler) DeleteWallet(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	walletID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if err := h.biz.DeleteWallet(c.Request().Context(), accountbiz.DeleteWalletParams{
		AccountID: claims.Account.ID,
		WalletID:  walletID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Wallet deleted")
}

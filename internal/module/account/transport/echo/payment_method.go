package accountecho

import (
	"encoding/json"
	"net/http"

	accountbiz "shopnexus-server/internal/module/account/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type CreatePaymentMethodRequest struct {
	Type      string          `json:"type" validate:"required"`
	Label     string          `json:"label" validate:"required"`
	Data      json.RawMessage `json:"data" validate:"required"`
	IsDefault bool            `json:"is_default"`
}

func (h *Handler) CreatePaymentMethod(c echo.Context) error {
	var req CreatePaymentMethodRequest
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

	result, err := h.biz.CreatePaymentMethod(c.Request().Context(), accountbiz.CreatePaymentMethodParams{
		Account:   claims.Account,
		Type:      req.Type,
		Label:     req.Label,
		Data:      req.Data,
		IsDefault: req.IsDefault,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListPaymentMethodRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListPaymentMethod(c echo.Context) error {
	var req ListPaymentMethodRequest
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

	result, err := h.biz.ListPaymentMethod(c.Request().Context(), accountbiz.ListPaymentMethodParams{
		Account:          claims.Account,
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type UpdatePaymentMethodRequest struct {
	ID    uuid.UUID       `json:"id" validate:"required"`
	Type  null.String     `json:"type" validate:"omitnil"`
	Label null.String     `json:"label" validate:"omitnil"`
	Data  json.RawMessage `json:"data" validate:"omitempty"`
}

func (h *Handler) UpdatePaymentMethod(c echo.Context) error {
	var req UpdatePaymentMethodRequest
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

	result, err := h.biz.UpdatePaymentMethod(c.Request().Context(), accountbiz.UpdatePaymentMethodParams{
		Account: claims.Account,
		ID:      req.ID,
		Type:    req.Type,
		Label:   req.Label,
		Data:    req.Data,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type DeletePaymentMethodRequest struct {
	ID uuid.UUID `json:"id" validate:"required"`
}

func (h *Handler) DeletePaymentMethod(c echo.Context) error {
	var req DeletePaymentMethodRequest
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

	if err := h.biz.DeletePaymentMethod(c.Request().Context(), accountbiz.DeletePaymentMethodParams{
		Account: claims.Account,
		ID:      req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Payment method deleted successfully")
}

type SetDefaultPaymentMethodRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) SetDefaultPaymentMethod(c echo.Context) error {
	var req SetDefaultPaymentMethodRequest
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

	result, err := h.biz.SetDefaultPaymentMethod(c.Request().Context(), accountbiz.SetDefaultPaymentMethodParams{
		Account: claims.Account,
		ID:      req.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

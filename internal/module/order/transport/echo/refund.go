package orderecho

import (
	"net/http"

	orderbiz "shopnexus-remastered/internal/module/order/biz"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type CreateRefundRequest struct {
	OrderID     uuid.UUID                 `json:"order_id" validate:"required"`
	Method      orderdb.OrderRefundMethod `json:"method" validate:"required,validateFn=Valid"`
	Reason      string                    `json:"reason" validate:"required,max=500"`
	Address     null.String               `json:"address" validate:"omitnil,max=500"`
	ResourceIDs []uuid.UUID               `json:"resource_ids" validate:"dive"`
}

func (h *Handler) CreateRefund(c echo.Context) error {
	var req CreateRefundRequest
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

	result, err := h.biz.CreateRefund(c.Request().Context(), orderbiz.CreateRefundParams{
		Account:     claims.Account,
		OrderID:     req.OrderID,
		Method:      req.Method,
		Reason:      req.Reason,
		Address:     req.Address,
		ResourceIDs: req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListRefundsRequest struct {
	commonmodel.PaginationParams
}

func (h *Handler) ListRefunds(c echo.Context) error {
	var req ListRefundsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListRefunds(c.Request().Context(), orderbiz.ListRefundsParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type UpdateRefundRequest struct {
	RefundID    uuid.UUID                 `json:"id" validate:"required"`
	Method      orderdb.OrderRefundMethod `json:"method" validate:"omitempty,validateFn=Valid"`
	Address     null.String               `json:"address" validate:"omitnil,max=500"`
	Reason      null.String               `json:"reason" validate:"omitnil,max=500"`
	ResourceIDs []uuid.UUID               `json:"resource_ids" validate:"required,dive"`
}

func (h *Handler) UpdateRefund(c echo.Context) error {
	var req UpdateRefundRequest
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

	result, err := h.biz.UpdateRefund(c.Request().Context(), orderbiz.UpdateRefundParams{
		Account:     claims.Account,
		RefundID:    req.RefundID,
		Method:      req.Method,
		Address:     req.Address,
		Reason:      req.Reason,
		ResourceIDs: req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type CancelRefundRequest struct {
	RefundID uuid.UUID `json:"id" validate:"required"`
}

func (h *Handler) CancelRefund(c echo.Context) error {
	var req CancelRefundRequest
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

	if err := h.biz.CancelRefund(c.Request().Context(), orderbiz.CancelRefundParams{
		Account:  claims.Account,
		RefundID: req.RefundID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return c.NoContent(http.StatusOK)
}

type ConfirmRefundRequest struct {
	RefundID uuid.UUID `json:"id" validate:"required"`
}

func (h *Handler) ConfirmRefund(c echo.Context) error {
	var req ConfirmRefundRequest
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

	refund, err := h.biz.ConfirmRefund(c.Request().Context(), orderbiz.ConfirmRefundParams{
		Account:  claims.Account,
		RefundID: req.RefundID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, refund)
}

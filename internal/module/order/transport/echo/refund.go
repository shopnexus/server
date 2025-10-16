package orderecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type CreateRefundRequest struct {
	OrderItemID int64                `json:"order_item_id" validate:"required"`
	Method      db.OrderRefundMethod `json:"method" validate:"required,validateFn=Valid"`
	Reason      string               `json:"reason" validate:"required,max=500"`
	Address     null.String          `json:"address" validate:"omitnil,max=500"`
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
		OrderItemID: req.OrderItemID,
		Method:      req.Method,
		Reason:      req.Reason,
		Address:     req.Address,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListRefundsRequest struct {
	sharedmodel.PaginationParams
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

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateRefundRequest struct {
	RefundID int64                `json:"id" validate:"required"`
	Method   db.OrderRefundMethod `json:"method" validate:"omitempty,validateFn=Valid"`
	Address  null.String          `json:"address" validate:"omitnil,max=500"`
	Reason   null.String          `json:"reason" validate:"omitnil,max=500"`
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
		Account:  claims.Account,
		RefundID: req.RefundID,
		Method:   req.Method,
		Address:  req.Address,
		Reason:   req.Reason,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type CancelRefundRequest struct {
	RefundID int64 `json:"id" validate:"required"`
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
	RefundID int64 `json:"id" validate:"required"`
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

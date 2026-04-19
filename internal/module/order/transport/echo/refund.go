package orderecho

import (
	"net/http"

	restateclient "shopnexus-server/internal/infras/restate"

	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type CreateBuyerRefundRequest struct {
	OrderID     uuid.UUID                 `json:"order_id"     validate:"required"`
	Method      orderdb.OrderRefundMethod `json:"method"       validate:"required,validateFn=Valid"`
	Reason      string                    `json:"reason"       validate:"required,max=500"`
	Address     null.String               `json:"address"      validate:"omitnil,max=500"`
	ResourceIDs []uuid.UUID               `json:"resource_ids" validate:"dive"`
}

func (h *Handler) CreateBuyerRefund(c echo.Context) error {
	var req CreateBuyerRefundRequest
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

	result, err := h.biz.CreateBuyerRefund(c.Request().Context(), orderbiz.CreateBuyerRefundParams{
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

type ListBuyerRefundsRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerRefunds(c echo.Context) error {
	var req ListBuyerRefundsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListBuyerRefunds(c.Request().Context(), orderbiz.ListBuyerRefundsParams{
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type UpdateBuyerRefundRequest struct {
	RefundID    uuid.UUID                 `json:"id"           validate:"required"`
	Method      orderdb.OrderRefundMethod `json:"method"       validate:"omitempty,validateFn=Valid"`
	Address     null.String               `json:"address"      validate:"omitnil,max=500"`
	Reason      null.String               `json:"reason"       validate:"omitnil,max=500"`
	ResourceIDs []uuid.UUID               `json:"resource_ids" validate:"required,dive"`
}

func (h *Handler) UpdateBuyerRefund(c echo.Context) error {
	var req UpdateBuyerRefundRequest
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

	result, err := restateclient.CallObject[ordermodel.Refund](c.Request().Context(), h.restate, "RefundLock", req.RefundID.String(), "UpdateBuyerRefund", orderbiz.UpdateBuyerRefundParams{
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

type CancelBuyerRefundRequest struct {
	RefundID uuid.UUID `json:"id" validate:"required"`
}

func (h *Handler) CancelBuyerRefund(c echo.Context) error {
	var req CancelBuyerRefundRequest
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

	if err := restateclient.SendObject(c.Request().Context(), h.restate, "RefundLock", req.RefundID.String(), "CancelBuyerRefund", orderbiz.CancelBuyerRefundParams{
		Account:  claims.Account,
		RefundID: req.RefundID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return c.NoContent(http.StatusOK)
}

type ConfirmSellerRefundRequest struct {
	RefundID uuid.UUID `json:"id" validate:"required"`
}

func (h *Handler) ConfirmSellerRefund(c echo.Context) error {
	var req ConfirmSellerRefundRequest
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

	refund, err := restateclient.CallObject[ordermodel.Refund](c.Request().Context(), h.restate, "RefundLock", req.RefundID.String(), "ConfirmSellerRefund", orderbiz.ConfirmSellerRefundParams{
		Account:  claims.Account,
		RefundID: req.RefundID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, refund)
}

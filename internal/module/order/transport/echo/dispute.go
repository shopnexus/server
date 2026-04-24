package orderecho

import (
	"net/http"

	orderbiz "shopnexus-server/internal/module/order/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type CreateRefundDisputeRequest struct {
	Reason string `json:"reason" validate:"required,min=1,max=1000"`
	Note   string `json:"note"   validate:"required,min=1,max=2000"`
}

func (h *Handler) CreateRefundDispute(c echo.Context) error {
	refundID, err := uuid.Parse(c.Param("refundID"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	var req CreateRefundDisputeRequest
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

	result, err := h.biz.CreateRefundDispute(c.Request().Context(), orderbiz.CreateRefundDisputeParams{
		Account:  claims.Account,
		RefundID: refundID,
		Reason:   req.Reason,
		Note:     req.Note,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListRefundDisputesRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListRefundDisputes(c echo.Context) error {
	var req ListRefundDisputesRequest
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

	result, err := h.biz.ListRefundDisputes(c.Request().Context(), orderbiz.ListRefundDisputesParams{
		Account:          claims.Account,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) ListRefundDisputesByRefund(c echo.Context) error {
	refundID, err := uuid.Parse(c.Param("refundID"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	var req ListRefundDisputesRequest
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

	result, err := h.biz.ListRefundDisputes(c.Request().Context(), orderbiz.ListRefundDisputesParams{
		Account:          claims.Account,
		RefundID:         uuid.NullUUID{UUID: refundID, Valid: true},
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type GetRefundDisputeRequest struct {
	DisputeID uuid.UUID `param:"disputeID" validate:"required"`
}

func (h *Handler) GetRefundDispute(c echo.Context) error {
	disputeID, err := uuid.Parse(c.Param("disputeID"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.GetRefundDispute(c.Request().Context(), orderbiz.GetRefundDisputeParams{
		Account:   claims.Account,
		DisputeID: disputeID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

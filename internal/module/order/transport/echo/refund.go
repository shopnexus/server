package orderecho

import (
	"net/http"

	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type CreateBuyerRefundRequest struct {
	OrderID               uuid.UUID                 `json:"order_id"                validate:"required"`
	Method                orderdb.OrderRefundMethod `json:"method"                  validate:"required,validateFn=Valid"`
	Reason                string                    `json:"reason"                  validate:"required,min=1,max=1000"`
	Address               string                    `json:"address"                 validate:"omitempty,max=500"`
	ReturnTransportOption string                    `json:"return_transport_option" validate:"max=100"`
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
		Account:               claims.Account,
		OrderID:               req.OrderID,
		Method:                req.Method,
		Reason:                req.Reason,
		Address:               req.Address,
		ReturnTransportOption: req.ReturnTransportOption,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListBuyerRefundsRequest struct {
	sharedmodel.PaginationParams
}

type ListSellerRefundsRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListSellerRefunds(c echo.Context) error {
	var req ListSellerRefundsRequest
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

	result, err := h.biz.ListSellerRefunds(c.Request().Context(), orderbiz.ListSellerRefundsParams{
		SellerID:         claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) ListBuyerRefunds(c echo.Context) error {
	var req ListBuyerRefundsRequest
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

	result, err := h.biz.ListBuyerRefunds(c.Request().Context(), orderbiz.ListBuyerRefundsParams{
		BuyerID:          claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) AcceptRefundStage1(c echo.Context) error {
	refundID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.AcceptRefundStage1(c.Request().Context(), orderbiz.AcceptRefundStage1Params{
		Account:  claims.Account,
		RefundID: refundID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

func (h *Handler) ApproveRefundStage2(c echo.Context) error {
	refundID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.ApproveRefundStage2(c.Request().Context(), orderbiz.ApproveRefundStage2Params{
		Account:  claims.Account,
		RefundID: refundID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type RejectRefundRequest struct {
	Stage         int    `json:"stage"          validate:"required,oneof=1 2"`
	RejectionNote string `json:"rejection_note" validate:"required,min=1,max=1000"`
}

func (h *Handler) RejectRefund(c echo.Context) error {
	refundID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	var req RejectRefundRequest
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

	result, err := h.biz.RejectRefund(c.Request().Context(), orderbiz.RejectRefundParams{
		Account:       claims.Account,
		RefundID:      refundID,
		Stage:         req.Stage,
		RejectionNote: req.RejectionNote,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

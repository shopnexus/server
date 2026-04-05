package orderecho

import (
	"net/http"

	orderbiz "shopnexus-server/internal/module/order/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListSellerPendingRequest struct {
	Search null.String `query:"search"`
	sharedmodel.PaginationParams
}

func (h *Handler) ListSellerPending(c echo.Context) error {
	var req ListSellerPendingRequest
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

	result, err := h.biz.ListSellerPending(c.Request().Context(), orderbiz.ListSellerPendingParams{
		SellerID:         claims.Account.ID,
		Search:           req.Search,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type QuoteTransportRequest struct {
	ItemIDs         []int64 `json:"item_ids" validate:"required,min=1"`
	TransportOption string  `json:"transport_option" validate:"required,min=1,max=100"`
}

func (h *Handler) QuoteTransport(c echo.Context) error {
	var req QuoteTransportRequest
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

	result, err := h.biz.QuoteTransport(c.Request().Context(), orderbiz.QuoteTransportParams{
		Account:         claims.Account,
		ItemIDs:         req.ItemIDs,
		TransportOption: req.TransportOption,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ConfirmSellerPendingRequest struct {
	ItemIDs         []int64 `json:"item_ids" validate:"required,min=1"`
	TransportOption string  `json:"transport_option" validate:"required,min=1,max=100"`
	Note            string  `json:"note" validate:"max=500"`
}

func (h *Handler) ConfirmSellerPending(c echo.Context) error {
	var req ConfirmSellerPendingRequest
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

	result, err := h.biz.ConfirmSellerPending(c.Request().Context(), orderbiz.ConfirmSellerPendingParams{
		Account:         claims.Account,
		ItemIDs:         req.ItemIDs,
		TransportOption: req.TransportOption,
		Note:            req.Note,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type RejectSellerPendingRequest struct {
	ItemIDs []int64 `json:"item_ids" validate:"required,min=1"`
}

func (h *Handler) RejectSellerPending(c echo.Context) error {
	var req RejectSellerPendingRequest
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

	if err := h.biz.RejectSellerPending(c.Request().Context(), orderbiz.RejectSellerPendingParams{
		Account: claims.Account,
		ItemIDs: req.ItemIDs,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Items rejected successfully")
}

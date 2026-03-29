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

type ListIncomingItemsRequest struct {
	Search null.String `query:"search"`
	sharedmodel.PaginationParams
}

func (h *Handler) ListIncomingItems(c echo.Context) error {
	var req ListIncomingItemsRequest
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

	result, err := h.biz.ListIncomingItems(c.Request().Context(), orderbiz.ListIncomingItemsParams{
		SellerID:         claims.Account.ID,
		Search:           req.Search,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ConfirmItemsRequest struct {
	ItemIDs         []int64 `json:"item_ids" validate:"required,min=1"`
	TransportOption string  `json:"transport_option" validate:"required,min=1,max=100"`
	Note            string  `json:"note" validate:"max=500"`
}

func (h *Handler) ConfirmItems(c echo.Context) error {
	var req ConfirmItemsRequest
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

	result, err := h.biz.ConfirmItems(c.Request().Context(), orderbiz.ConfirmItemsParams{
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

type RejectItemsRequest struct {
	ItemIDs []int64 `json:"item_ids" validate:"required,min=1"`
}

func (h *Handler) RejectItems(c echo.Context) error {
	var req RejectItemsRequest
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

	if err := h.biz.RejectItems(c.Request().Context(), orderbiz.RejectItemsParams{
		Account: claims.Account,
		ItemIDs: req.ItemIDs,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Items rejected successfully")
}

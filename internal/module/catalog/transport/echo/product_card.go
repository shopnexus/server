package catalogecho

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"
)

type ListProductCardRequest struct {
	commonmodel.PaginationParams
	VendorID uuid.NullUUID `query:"vendor_id" validate:"omitnil"`
	Search   null.String   `query:"search" validate:"omitnil"`
}

func (h *Handler) ListProductCard(c echo.Context) error {
	var req ListProductCardRequest

	// TODO: improve binder error message (currently it not show which field has error)
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductCard(c.Request().Context(), catalogbiz.ListProductCardParams{
		PaginationParams: req.PaginationParams,
		VendorID:         req.VendorID,
		Search:           req.Search,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ListRecommendedProductCardParams struct {
	Limit int `query:"limit" validate:"omitempty"`
}

func (h *Handler) ListRecommendedProductCard(c echo.Context) error {
	var req ListRecommendedProductCardParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, _ := authclaims.GetClaims(c.Request())

	result, err := h.biz.ListRecommendedProductCard(c.Request().Context(), catalogbiz.ListRecommendedProductCardParams{
		Account: claims.Account,
		Limit:   req.Limit,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

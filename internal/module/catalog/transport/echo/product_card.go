package catalogecho

import (
	"net/http"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"

	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
)

type ListProductCardRequest struct {
	sharedmodel.PaginationParams
	Search null.String `query:"search" validate:"omitnil"`
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

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.ListRecommendedProductCard(c.Request().Context(), catalogbiz.ListRecommendedProductCardParams{
		Account: claims.Account,
		Limit:   req.Limit,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

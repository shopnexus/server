package catalogecho

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"

	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	authclaims "shopnexus-server/internal/shared/claims"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"
)

type ListProductCardRequest struct {
	commonmodel.PaginationParams
	VendorID uuid.NullUUID `query:"vendor_id" validate:"omitnil"`
	Search   null.String   `query:"search" validate:"omitnil"`
}

func (h *Handler) ListProductCard(c echo.Context) error {
	var req ListProductCardRequest

	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	params := catalogbiz.ListProductCardParams{
		PaginationParams: req.PaginationParams.Constrain(),
		VendorID:         req.VendorID,
		Search:           req.Search,
	}

	if claims, err := authclaims.GetClaims(c.Request()); err == nil {
		params.AccountID = &claims.Account.ID
	}

	result, err := h.biz.ListProductCard(c.Request().Context(), params)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}

func (h *Handler) GetProductCard(c echo.Context) error {
	spuID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	params := catalogbiz.GetProductCardParams{
		SpuID: spuID,
	}

	if claims, err := authclaims.GetClaims(c.Request()); err == nil {
		params.AccountID = &claims.Account.ID
	}

	result, err := h.biz.GetProductCard(c.Request().Context(), params)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
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

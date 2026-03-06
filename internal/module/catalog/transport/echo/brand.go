package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type GetBrandRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetBrand(c echo.Context) error {
	var req GetBrandRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListBrand(c.Request().Context(), catalogbiz.ListBrandParams{
		PaginationParams: commonmodel.PaginationParams{
			Limit: null.Int32From(1),
		}.Constrain(),
		ID: []uuid.UUID{req.ID},
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	if len(result.Data) == 0 {
		return response.FromError(c.Response().Writer, http.StatusNotFound, echo.NewHTTPError(http.StatusNotFound, "brand not found"))
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result.Data[0])
}

type ListBrandRequest struct {
	commonmodel.PaginationParams
	Search null.String `query:"search" validate:"omitnil"`
}

func (h *Handler) ListBrand(c echo.Context) error {
	var req ListBrandRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListBrand(c.Request().Context(), catalogbiz.ListBrandParams{
		PaginationParams: req.PaginationParams.Constrain(),
		Search:           req.Search,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

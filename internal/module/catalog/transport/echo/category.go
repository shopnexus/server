package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListCategoryRequest struct {
	sharedmodel.PaginationParams

	ID     []uuid.UUID `query:"id"     validate:"omitempty,dive,gt=0"`
	Search null.String `query:"search" validate:"omitnil"`
}

func (h *Handler) ListCategory(c echo.Context) error {
	var req ListCategoryRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListCategory(c.Request().Context(), catalogbiz.ListCategoryParams{
		PaginationParams: req.PaginationParams.Constrain(),
		ID:               req.ID,
		Search:           req.Search,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}

type GetCategoryRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetCategory(c echo.Context) error {
	var req GetCategoryRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListCategory(c.Request().Context(), catalogbiz.ListCategoryParams{
		PaginationParams: sharedmodel.PaginationParams{
			Limit: null.Int32From(1),
		}.Constrain(),
		ID: []uuid.UUID{req.ID},
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result.Data[0])
}

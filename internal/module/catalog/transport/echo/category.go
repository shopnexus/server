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

type ListCategoryRequest struct {
	commonmodel.PaginationParams
	ID     []uuid.UUID `query:"id" validate:"omitempty,dive,gt=0"`
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
		PaginationParams: req.PaginationParams,
		ID:               req.ID,
		Search:           req.Search,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

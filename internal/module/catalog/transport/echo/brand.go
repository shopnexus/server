package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/response"

	"github.com/labstack/echo/v4"
)

type ListBrandRequest struct {
	commonmodel.PaginationParams
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
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

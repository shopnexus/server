package catalogecho

import (
	"net/http"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type ListProductCardRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListProductCard(c echo.Context) error {
	var req ListProductCardRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductCard(c.Request().Context(), catalogbiz.ListProductCardParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

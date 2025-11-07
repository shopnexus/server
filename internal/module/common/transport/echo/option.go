package commonecho

import (
	"net/http"

	commonbiz "shopnexus-remastered/internal/module/common/biz"
	"shopnexus-remastered/internal/module/shared/response"

	"github.com/labstack/echo/v4"
)

type ListServiceOptionRequest struct {
	Category string `query:"category" validate:"required"`
}

func (h *Handler) ListServiceOption(c echo.Context) error {
	var req ListServiceOptionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListServiceOption(c.Request().Context(), commonbiz.ListServiceOptionParams{
		Category: []string{req.Category},
		IsActive: []bool{true},
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

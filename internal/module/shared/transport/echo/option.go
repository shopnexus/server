package sharedecho

import (
	"net/http"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

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

	result, err := h.biz.ListServiceOption(c.Request().Context(), sharedbiz.ListServiceOptionParams{
		Category: []string{req.Category},
		IsActive: []bool{true},
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

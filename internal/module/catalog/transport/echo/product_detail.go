package catalogecho

import (
	"net/http"

	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type GetProductDetailRequest struct {
	ID uuid.UUID `query:"id" validate:"required"`
}

func (h *Handler) GetProductDetail(c echo.Context) error {
	var req GetProductDetailRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.GetProductDetail(c.Request().Context(), req.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

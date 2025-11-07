package inventoryecho

import (
	"net/http"

	commonmodel "shopnexus-remastered/internal/module/common/model"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	"shopnexus-remastered/internal/module/shared/response"

	"github.com/labstack/echo/v4"
)

type ListProductSerialRequest struct {
	commonmodel.PaginationParams
	SkuID int64 `query:"sku_id" validate:"required,gt=0"`
}

func (h *Handler) ListProductSerial(c echo.Context) error {
	var req ListProductSerialRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductSerial(c.Request().Context(), inventorybiz.ListProductSerialParams{
		PaginationParams: req.PaginationParams,
		SkuID:            req.SkuID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

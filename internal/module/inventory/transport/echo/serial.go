package inventoryecho

import (
	"net/http"

	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type ListProductSerialRequest struct {
	commonmodel.PaginationParams
	RefID   uuid.UUID                         `query:"ref_id" validate:"required"`
	RefType inventorydb.InventoryStockRefType `query:"ref_type" validate:"required,validateFn=Valid"`
}

func (h *Handler) ListSerial(c echo.Context) error {
	var req ListProductSerialRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListSerial(c.Request().Context(), inventorybiz.ListSerialParams{
		PaginationParams: req.PaginationParams.Constrain(),
		RefID:            req.RefID,
		RefType:          req.RefType,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

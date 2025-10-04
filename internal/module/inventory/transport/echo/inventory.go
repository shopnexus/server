package inventoryecho

import (
	"net/http"

	"shopnexus-remastered/internal/db"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *inventorybiz.InventoryBiz
}

func NewHandler(e *echo.Echo, biz *inventorybiz.InventoryBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/inventory")
	api.POST("/add", h.AddStock)
	return h
}

type AddStockRequest struct {
	RefID     int64                    `validate:"required,gt=0"`
	RefType   db.InventoryStockRefType `validate:"required,validFn=Valid"`
	Change    int64                    `validate:"required,gt=0"`
	SerialIDs []string                 `validate:"dive,required"`
}

func (h *Handler) AddStock(c echo.Context) error {
	var req AddStockRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if err := h.biz.AddStock(c.Request().Context(), inventorybiz.AddStockParams{
		RefID:     req.RefID,
		RefType:   req.RefType,
		Change:    req.Change,
		SerialIDs: req.SerialIDs,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "add stock successfully")
}

type UpdateSkuSerialRequest struct {
	SerialIDs []string
	Status    db.InventoryProductStatus `validate:"required,validFn=Valid"`
}

func (h *Handler) UpdateSkuSerial(c echo.Context) error {
	var req UpdateSkuSerialRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if err := h.biz.UpdateSkuSerial(c.Request().Context(), inventorybiz.UpdateSkuSerialParams{
		SerialIDs: req.SerialIDs,
		Status:    req.Status,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "update sku serial successfully")
}

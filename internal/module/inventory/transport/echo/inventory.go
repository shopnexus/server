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

type Handler struct {
	biz *inventorybiz.InventoryBiz
}

func NewHandler(e *echo.Echo, biz *inventorybiz.InventoryBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/inventory")

	stockApi := api.Group("/stock")
	stockApi.GET("", h.GetStock)
	stockApi.GET("/history", h.ListStockHistory)
	stockApi.POST("/import", h.ImportStock)

	serialApi := api.Group("/serial")
	serialApi.GET("", h.ListProductSerial)
	serialApi.PATCH("", h.UpdateSkuSerial)
	return h
}

type GetStockRequest struct {
	RefID   uuid.UUID                         `query:"ref_id" validate:"required"`
	RefType inventorydb.InventoryStockRefType `query:"ref_type" validate:"required,validateFn=Valid"`
}

func (h *Handler) GetStock(c echo.Context) error {
	var req GetStockRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.GetStock(c.Request().Context(), inventorybiz.GetStockParams{
		RefID:   req.RefID,
		RefType: req.RefType,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListStockHistoryRequest struct {
	commonmodel.PaginationParams
	RefID   uuid.UUID                         `query:"ref_id" validate:"required"`
	RefType inventorydb.InventoryStockRefType `query:"ref_type" validate:"required,validateFn=Valid"`
}

func (h *Handler) ListStockHistory(c echo.Context) error {
	var req ListStockHistoryRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListStockHistory(c.Request().Context(), inventorybiz.ListStockHistoryParams{
		PaginationParams: req.PaginationParams,
		RefID:            req.RefID,
		RefType:          req.RefType,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ImportStockRequest struct {
	RefID     uuid.UUID                         `json:"ref_id" validate:"required"`
	RefType   inventorydb.InventoryStockRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	Change    int64                             `json:"change" validate:"required,gt=0"`
	SerialIDs []string                          `json:"serial_ids" validate:"dive,required"`
}

func (h *Handler) ImportStock(c echo.Context) error {
	var req ImportStockRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if err := h.biz.ImportStock(c.Request().Context(), inventorybiz.ImportStockParams{
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
	SerialIDs []string                           `json:"serial_ids" validate:"required,dive,required"`
	Status    inventorydb.InventoryProductStatus `json:"status" validate:"required,validateFn=Valid"`
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

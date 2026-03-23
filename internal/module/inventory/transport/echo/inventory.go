package inventoryecho

import (
	"net/http"

	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz inventorybiz.InventoryClient
}

func NewHandler(e *echo.Echo, biz inventorybiz.InventoryClient) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/inventory")

	stockApi := api.Group("/stock")
	stockApi.GET("", h.GetStock)
	stockApi.GET("/history", h.ListStockHistory)
	stockApi.POST("/import", h.ImportStock)

	serialApi := api.Group("/serial")
	serialApi.GET("", h.ListSerial)
	serialApi.PATCH("", h.UpdateSerial)
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
	sharedmodel.PaginationParams
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
		PaginationParams: req.PaginationParams.Constrain(),
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

type ListProductSerialRequest struct {
	sharedmodel.PaginationParams
	StockID int64 `query:"stock_id" validate:"required,gt=0"`
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
		StockID:          req.StockID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type UpdateSerialRequest struct {
	SerialIDs []string                    `json:"serial_ids" validate:"required,dive,required"`
	Status    inventorydb.InventoryStatus `json:"status" validate:"required,validateFn=Valid"`
}

func (h *Handler) UpdateSerial(c echo.Context) error {
	var req UpdateSerialRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if err := h.biz.UpdateSerial(c.Request().Context(), inventorybiz.UpdateSerialParams{
		SerialIDs: req.SerialIDs,
		Status:    req.Status,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "update sku serial successfully")
}

package inventoryecho

import (
	"net/http"

	"shopnexus-remastered/internal/db"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *inventorybiz.InventoryBiz
}

func NewHandler(e *echo.Echo, biz *inventorybiz.InventoryBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/inventory")

	api.GET("/serial", h.ListProductSerial)
	api.GET("/stock", h.GetStock)
	api.GET("/stock-history", h.ListStockHistory)
	api.POST("/import", h.ImportStock)
	api.PATCH("/sku-serial", h.UpdateSkuSerial)
	return h
}

type GetStockRequest struct {
	RefID   int64                    `query:"ref_id" validate:"required,gt=0"`
	RefType db.InventoryStockRefType `query:"ref_type" validate:"required,validateFn=Valid"`
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
	RefID   int64                    `query:"ref_id" validate:"required,gt=0"`
	RefType db.InventoryStockRefType `query:"ref_type" validate:"required,validateFn=Valid"`
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
	RefID     int64                    `json:"ref_id" validate:"required,gt=0"`
	RefType   db.InventoryStockRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	Change    int64                    `json:"change" validate:"required,gt=0"`
	SerialIDs []string                 `json:"serial_ids" validate:"dive,required"`
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
	SerialIDs []string                  `json:"serial_ids" validate:"required,dive,required"`
	Status    db.InventoryProductStatus `json:"status" validate:"required,validateFn=Valid"`
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

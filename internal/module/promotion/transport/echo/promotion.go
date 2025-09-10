package catalogecho

import (
	"net/http"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *promotionbiz.PromotionBiz
}

func NewHandler(e *echo.Echo, biz *promotionbiz.PromotionBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/catalog")
	_ = api

	return h
}

type GetPromotionRequest struct {
	ID int64 `query:"id" validate:"required,gt=0"`
}

func (h *Handler) GetPromotion(c echo.Context) error {
	var req GetPromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.GetPromotion(c.Request().Context(), promotionbiz.GetPromotionParams{
		ID: req.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListPromotionRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListPromotion(c echo.Context) error {
	var req ListPromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListPromotion(c.Request().Context(), promotionbiz.ListPromotionParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

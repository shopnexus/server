package searchecho

import (
	"net/http"

	"github.com/labstack/echo/v4"

	searchbiz "shopnexus-remastered/internal/module/search/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
)

type Handler struct {
	biz *searchbiz.SearchBiz
}

func NewHandler(e *echo.Echo, biz *searchbiz.SearchBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/search")
	api.GET("/product", h.SearchProduct)

	return h
}

type SearchProductRequest struct {
	sharedmodel.PaginationParams
	Query string `query:"query" validate:"required"`
}

func (h *Handler) SearchProduct(c echo.Context) error {
	var req SearchProductRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.Search(c.Request().Context(), searchbiz.SearchParams{
		PaginationParams: req.PaginationParams,
		Collection:       "products",
		Query:            req.Query,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

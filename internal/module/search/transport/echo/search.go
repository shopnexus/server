package searchecho

import (
	"github.com/labstack/echo/v4"

	searchbiz "shopnexus-remastered/internal/module/search/biz"
)

type Handler struct {
	biz *searchbiz.SearchBiz
}

func NewHandler(e *echo.Echo, biz *searchbiz.SearchBiz) *Handler {
	h := &Handler{biz: biz}
	// api := e.Group("/api/v1/search")
	// api.GET("/product", h.SearchProduct)

	return h
}

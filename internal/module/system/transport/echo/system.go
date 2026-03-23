package systemecho

import (
	systembiz "shopnexus-server/internal/module/system/biz"

	"github.com/labstack/echo/v4"
)

// Handler handles HTTP requests for the system module.
type Handler struct {
	biz *systembiz.SystemBiz
}

// NewHandler registers system module routes and returns the handler.
func NewHandler(e *echo.Echo, biz *systembiz.SystemBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/system")
	_ = api

	return h
}

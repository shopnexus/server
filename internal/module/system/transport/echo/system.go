package systemecho

import (
	systembiz "shopnexus-server/internal/module/system/biz"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *systembiz.SystemBiz
}

func NewHandler(e *echo.Echo, biz *systembiz.SystemBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/system")
	_ = api

	return h
}

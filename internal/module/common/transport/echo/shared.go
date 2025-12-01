package commonecho

import (
	commonbiz "shopnexus-remastered/internal/module/common/biz"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *commonbiz.CommonBiz
}

func NewHandler(e *echo.Echo, biz *commonbiz.CommonBiz) (*Handler, error) {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/common")

	api.POST("/files", h.UploadFile)
	api.GET("/option", h.ListServiceOption)
	// api.GET("/files/:object_key", h.GetFile)

	return h, nil
}

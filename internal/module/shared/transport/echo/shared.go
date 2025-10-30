package sharedecho

import (
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"

	"github.com/labstack/echo/v4"
)

const Endpoint = "/api/v1/shared/upload-file"

type Handler struct {
	biz *sharedbiz.SharedBiz
}

func NewHandler(e *echo.Echo, biz *sharedbiz.SharedBiz) (*Handler, error) {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/shared")

	api.POST("/files", h.UploadFile)
	// api.GET("/files/:object_key", h.GetFile)

	return h, nil
}

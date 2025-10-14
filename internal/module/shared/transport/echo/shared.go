package sharedecho

import (
	"net/http"

	sharedbiz "shopnexus-remastered/internal/module/shared/biz"

	"github.com/labstack/echo/v4"
	tusd "github.com/tus/tusd/v2/pkg/handler"
)

const Endpoint = "/api/v1/shared/upload-file"

type Handler struct {
	biz        *sharedbiz.SharedBiz
	tusHandler *tusd.Handler
}

func NewHandler(e *echo.Echo, biz *sharedbiz.SharedBiz) (*Handler, error) {
	var err error
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/shared")

	// Setup tus handler
	h.tusHandler, err = biz.NewTusHandler()
	if err != nil {
		return nil, err
	}
	api.Any("/files", echo.WrapHandler(http.StripPrefix("/api/v1/shared/files", h.tusHandler)))
	api.Any("/files/:id", echo.WrapHandler(http.StripPrefix("/api/v1/shared/files", h.tusHandler)))
	api.GET("/files/:file_code", h.GetFile)

	return h, nil
}

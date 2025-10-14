package sharedecho

import (
	"fmt"
	"net/http"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type GetFileRequest struct {
	ResourceCode string `param:"file_code" validate:"required"`
}

func (h *Handler) GetFile(c echo.Context) error {
	var req GetFileRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if config.GetConfig().Filestore.Type == "local" {
		// Manually serve the file using tus handler (because we disabled download in tus config before)
		c.Request().URL.Path = fmt.Sprintf("/%s", req.ResourceCode)
		h.tusHandler.GetFile(c.Response().Writer, c.Request())
		return nil
	}
	if config.GetConfig().Filestore.Type == "s3" {
		// Return redirect to cloudfront URL

		url := fmt.Sprintf("https://%s/%s", config.GetConfig().Filestore.S3.CloudfrontURL, req.ResourceCode)
		return c.Redirect(http.StatusSeeOther, url)
	}

	return response.FromError(c.Response().Writer, http.StatusInternalServerError, fmt.Errorf("unsupported filestore type"))
}

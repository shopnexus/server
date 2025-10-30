package sharedecho

import (
	"fmt"
	"net/http"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

// UploadFile handles simple multipart/form-data upload.
func (h *Handler) UploadFile(c echo.Context) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, fmt.Errorf("missing file: %w", err))
	}
	src, err := fileHeader.Open()
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	defer src.Close()

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	objectKey, publicURL, err := h.biz.UploadFile(c.Request().Context(), sharedbiz.UploadFileParams{
		Account:     claims.Account,
		File:        src,
		Filename:    fileHeader.Filename,
		ContentType: fileHeader.Header.Get("Content-Type"),
		Size:        fileHeader.Size,
		Private:     false, // TODO: let the client specify
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	// If no public URL (e.g., local store), build via GET endpoint using request host
	if publicURL == "" {
		publicURL = fmt.Sprintf("%s://%s/api/v1/shared/files/%s", c.Scheme(), c.Request().Host, objectKey)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]string{
		"key": objectKey,
		"url": publicURL,
	})
}

// TODO: remove this (no use)
type GetFileRequest struct {
	ObjectKey string `param:"object_key" validate:"required"`
}

func (h *Handler) GetFile(c echo.Context) error {
	var req GetFileRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	url, err := h.biz.GetFileURL(c.Request().Context(), "TODO: bruh", req.ObjectKey)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]string{
		"url": url,
	})
}

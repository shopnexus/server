package commonecho

import (
	"fmt"
	"net/http"

	commonbiz "shopnexus-remastered/internal/module/common/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	"shopnexus-remastered/internal/shared/response"

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

	private := c.FormValue("private")

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.UploadFile(c.Request().Context(), commonbiz.UploadFileParams{
		Account:     claims.Account,
		File:        src,
		Filename:    fileHeader.Filename,
		ContentType: fileHeader.Header.Get("Content-Type"),
		Size:        fileHeader.Size,
		Private:     private == "true",
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]any{
		"id":  result.ResourceID,
		"url": result.URL,
	})
}

// type GetFileRequest struct {
// 	ObjectKey string `param:"object_key" validate:"required"`
// }

// func (h *Handler) GetFile(c echo.Context) error {
// 	var req GetFileRequest
// 	if err := c.Bind(&req); err != nil {
// 		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
// 	}
// 	if err := c.Validate(&req); err != nil {
// 		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
// 	}

// 	url, err := h.biz.GetFileURL(c.Request().Context(), "TODO: bruh", req.ObjectKey)
// 	if err != nil {
// 		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
// 	}

// 	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]string{
// 		"url": url,
// 	})
// }

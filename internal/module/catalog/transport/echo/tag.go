package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/labstack/echo/v4"
)

type ListTagRequest struct {
	commonmodel.PaginationParams
}

func (h *Handler) ListTag(c echo.Context) error {
	var req ListTagRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListTag(c.Request().Context(), catalogbiz.ListTagParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type GetTagRequest struct {
	Tag string `param:"tag" validate:"required,min=1,max=100"`
}

func (h *Handler) GetTag(c echo.Context) error {
	var req GetTagRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.GetTag(c.Request().Context(), catalogbiz.GetTagParams{
		Account: claims.Account,
		Tag:     req.Tag,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

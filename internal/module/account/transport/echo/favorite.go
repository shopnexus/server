package accountecho

import (
	"net/http"

	accountbiz "shopnexus-remastered/internal/module/account/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type AddFavoriteRequest struct {
	SpuID uuid.UUID `param:"spu_id" validate:"required"`
}

func (h *Handler) AddFavorite(c echo.Context) error {
	var req AddFavoriteRequest
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

	result, err := h.biz.AddFavorite(c.Request().Context(), accountbiz.AddFavoriteParams{
		Account: claims.Account,
		SpuID:   req.SpuID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type RemoveFavoriteRequest struct {
	SpuID uuid.UUID `param:"spu_id" validate:"required"`
}

func (h *Handler) RemoveFavorite(c echo.Context) error {
	var req RemoveFavoriteRequest
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

	if err := h.biz.RemoveFavorite(c.Request().Context(), accountbiz.RemoveFavoriteParams{
		Account: claims.Account,
		SpuID:   req.SpuID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Favorite removed successfully")
}

type ListFavoriteRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListFavorite(c echo.Context) error {
	var req ListFavoriteRequest
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

	result, err := h.biz.ListFavorite(c.Request().Context(), accountbiz.ListFavoriteParams{
		Account:          claims.Account,
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CheckFavoriteRequest struct {
	SpuID uuid.UUID `param:"spu_id" validate:"required"`
}

func (h *Handler) CheckFavorite(c echo.Context) error {
	var req CheckFavoriteRequest
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

	isFavorited, err := h.biz.CheckFavorite(c.Request().Context(), accountbiz.CheckFavoriteParams{
		Account: claims.Account,
		SpuID:   req.SpuID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]bool{"is_favorited": isFavorited})
}

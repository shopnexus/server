package catalogecho

import (
	"net/http"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSpuRequest struct {
	sharedmodel.PaginationParams
	Code       []string `query:"code" comma_separated:"true" validate:"omitempty"`
	CategoryID []int64  `query:"category_id" comma_separated:"true" validate:"omitempty"`
	BrandID    []int64  `query:"brand_id" comma_separated:"true" validate:"omitempty"`
	IsActive   []bool   `query:"is_active" comma_separated:"true" validate:"omitempty"`
}

func (h *Handler) ListProductSpu(c echo.Context) error {
	var req ListProductSpuRequest
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

	result, err := h.biz.ListProductSpu(c.Request().Context(), catalogbiz.ListProductSpuParams{
		PaginationParams: req.PaginationParams,
		Account:          claims.Account,
		Code:             req.Code,
		CategoryID:       req.CategoryID,
		BrandID:          req.BrandID,
		IsActive:         req.IsActive,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type GetProductSpuParams struct {
	ID int64 `param:"id" validate:"required,gt=0"`
}

func (h *Handler) GetProductSpu(c echo.Context) error {
	var req GetProductSpuParams
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

	result, err := h.biz.ListProductSpu(c.Request().Context(), catalogbiz.ListProductSpuParams{
		Account: claims.Account,
		ID:      []int64{req.ID},
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	if len(result.Data) == 0 {
		return response.FromError(c.Response().Writer, http.StatusNotFound, echo.NewHTTPError(http.StatusNotFound, "product spu not found"))
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result.Data[0])
}

type CreateProductSpuRequest struct {
	CategoryID  int64       `json:"category_id" validate:"required,gt=0"`
	BrandID     int64       `json:"brand_id" validate:"required,gt=0"`
	Name        string      `json:"name" validate:"required,min=1,max=200"`
	Description string      `json:"description" validate:"required,max=1000"`
	IsActive    bool        `json:"is_active" validate:"omitempty"`
	Tags        []string    `json:"tags" validate:"required,dive,min=1,max=100"`
	ResourceIDs []uuid.UUID `json:"resource_ids" validate:"omitempty,dive"`
}

func (h *Handler) CreateProductSpu(c echo.Context) error {
	var req CreateProductSpuRequest
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

	spu, err := h.biz.CreateProductSpu(c.Request().Context(), catalogbiz.CreateProductSpuParams{
		Account:     claims.Account,
		CategoryID:  req.CategoryID,
		BrandID:     req.BrandID,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    req.IsActive,
		Tags:        req.Tags,
		ResourceIDs: req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type UpdateProductSpuRequest struct {
	ID            int64       `json:"id" validate:"required,gt=0"`
	CategoryID    null.Int64  `json:"category_id" validate:"omitnil,gt=0"`
	FeaturedSkuID null.Int64  `json:"featured_sku_id" validate:"omitnil,gt=0"`
	BrandID       null.Int64  `json:"brand_id" validate:"omitnil,gt=0"`
	Name          null.String `json:"name" validate:"omitnil,min=1,max=200"`
	Description   null.String `json:"description" validate:"omitnil,max=1000"`
	IsActive      null.Bool   `json:"is_active" validate:"omitnil"`
	Tags          []string    `json:"tags" validate:"required,dive,min=1,max=100"`
	ResourceIDs   []uuid.UUID `json:"resource_ids" validate:"omitempty,dive"`
}

func (h *Handler) UpdateProductSpu(c echo.Context) error {
	var req UpdateProductSpuRequest
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

	spu, err := h.biz.UpdateProductSpu(c.Request().Context(), catalogbiz.UpdateProductSpuParams{
		Account:       claims.Account,
		ID:            req.ID,
		FeaturedSkuID: req.FeaturedSkuID,
		CategoryID:    req.CategoryID,
		BrandID:       req.BrandID,
		Name:          req.Name,
		Description:   req.Description,
		IsActive:      req.IsActive,
		Tags:          req.Tags,
		ResourceIDs:   req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type DeleteProductSpuRequest struct {
	ID int64 `param:"id" validate:"required,gt=0"`
}

func (h *Handler) DeleteProductSpu(c echo.Context) error {
	var req DeleteProductSpuRequest
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

	if err := h.biz.DeleteProductSpu(c.Request().Context(), catalogbiz.DeleteProductSpuParams{
		Account: claims.Account,
		ID:      req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "deleted")
}

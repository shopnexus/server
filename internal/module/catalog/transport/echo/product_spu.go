package catalogecho

import (
	"net/http"
	authbiz "shopnexus-remastered/internal/module/auth/biz"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
	"time"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Code       []string `query:"code" comma_separated:"true" validate:"required"`
	VendorID   []int64  `query:"vendor_id" comma_separated:"true" validate:"required"`
	CategoryID []int64  `query:"category_id" comma_separated:"true" validate:"required"`
	BrandID    []int64  `query:"brand_id" comma_separated:"true" validate:"required"`
	IsActive   []bool   `query:"is_active" comma_separated:"true" validate:"required"`
}

func (h *Handler) ListProductSpu(c echo.Context) error {
	var req ListProductSpuParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductSpu(c.Request().Context(), catalogbiz.ListProductSpuParams{
		PaginationParams: req.PaginationParams,
		Code:             req.Code,
		AccountID:        req.VendorID,
		CategoryID:       req.CategoryID,
		BrandID:          req.BrandID,
		IsActive:         req.IsActive,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CreateProductSpuParams struct {
	CategoryID       int64     `validate:"required,gt=0"`
	BrandID          int64     `validate:"required,gt=0"`
	Name             string    `validate:"required,min=1,max=200"`
	Description      string    `validate:"required,max=1000"`
	DateManufactured time.Time `validate:"required"`
}

func (h *Handler) CreateProductSpu(c echo.Context) error {
	var req CreateProductSpuParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	spu, err := h.biz.CreateProductSpu(c.Request().Context(), catalogbiz.CreateProductSpuParams{
		Account:          claims.Account,
		CategoryID:       req.CategoryID,
		BrandID:          req.BrandID,
		Name:             req.Name,
		Description:      req.Description,
		DateManufactured: req.DateManufactured,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type UpdateProductSpuParams struct {
	ID               int64       `validate:"required,gt=0"`
	CategoryID       null.Int64  `validate:"omitnil,gt=0"`
	FeaturedSkuID    null.Int64  `validate:"omitnil,gt=0"`
	BrandID          null.Int64  `validate:"omitnil,gt=0"`
	Name             null.String `validate:"omitnil,min=1,max=200"`
	Description      null.String `validate:"omitnil,max=1000"`
	DateManufactured null.Time   `validate:"omitnil"`
	IsActive         null.Bool   `validate:"omitnil"`
}

func (h *Handler) UpdateProductSpu(c echo.Context) error {
	var req UpdateProductSpuParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	spu, err := h.biz.UpdateProductSpu(c.Request().Context(), catalogbiz.UpdateProductSpuParams{
		Account:          claims.Account,
		ID:               req.ID,
		FeaturedSkuID:    req.FeaturedSkuID,
		CategoryID:       req.CategoryID,
		BrandID:          req.BrandID,
		Name:             req.Name,
		Description:      req.Description,
		DateManufactured: req.DateManufactured,
		IsActive:         req.IsActive,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type DeleteProductSpuParams struct {
	ID int64 `validate:"required,gt=0"`
}

func (h *Handler) DeleteProductSpu(c echo.Context) error {
	var req DeleteProductSpuParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	if err := h.biz.DeleteProductSpu(c.Request().Context(), catalogbiz.DeleteProductSpuParams{
		Account: claims.Account,
		ID:      req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, map[string]string{"status": "ok"})
}

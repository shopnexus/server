package catalogecho

import (
	"net/http"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSpuParams struct {
	sharedmodel.PaginationParams
	Code       []string `query:"code" comma_separated:"true" validate:"omitempty"`
	CategoryID []int64  `query:"category_id" comma_separated:"true" validate:"omitempty"`
	BrandID    []int64  `query:"brand_id" comma_separated:"true" validate:"omitempty"`
	IsActive   []bool   `query:"is_active" comma_separated:"true" validate:"omitempty"`
}

func (h *Handler) ListProductSpu(c echo.Context) error {
	var req ListProductSpuParams
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

type CreateProductSpuParams struct {
	CategoryID  int64  `validate:"required,gt=0"`
	BrandID     int64  `validate:"required,gt=0"`
	Name        string `validate:"required,min=1,max=200"`
	Description string `validate:"required,max=1000"`
}

func (h *Handler) CreateProductSpu(c echo.Context) error {
	var req CreateProductSpuParams
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
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type UpdateProductSpuParams struct {
	ID            int64       `validate:"required,gt=0"`
	CategoryID    null.Int64  `validate:"omitnil,gt=0"`
	FeaturedSkuID null.Int64  `validate:"omitnil,gt=0"`
	BrandID       null.Int64  `validate:"omitnil,gt=0"`
	Name          null.String `validate:"omitnil,min=1,max=200"`
	Description   null.String `validate:"omitnil,max=1000"`
	IsActive      null.Bool   `validate:"omitnil"`
}

func (h *Handler) UpdateProductSpu(c echo.Context) error {
	var req UpdateProductSpuParams
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

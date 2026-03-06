package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSpuRequest struct {
	commonmodel.PaginationParams
	Code       []string    `query:"code" comma_separated:"true" validate:"omitempty"`
	CategoryID []uuid.UUID `query:"category_id" comma_separated:"true" validate:"omitempty"`
	BrandID    []uuid.UUID `query:"brand_id" comma_separated:"true" validate:"omitempty"`
	IsActive   []bool      `query:"is_active" comma_separated:"true" validate:"omitempty"`
}

func (h *Handler) ListProductSpu(c echo.Context) error {
	var req ListProductSpuRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	// claims, err := authclaims.GetClaims(c.Request())
	// if err != nil {
	// 	return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	// }

	result, err := h.biz.ListProductSpu(c.Request().Context(), catalogbiz.ListProductSpuParams{
		PaginationParams: req.PaginationParams.Constrain(),
		Slug:             req.Code,
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
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetProductSpu(c echo.Context) error {
	var req GetProductSpuParams
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductSpu(c.Request().Context(), catalogbiz.ListProductSpuParams{
		ID: []uuid.UUID{req.ID},
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
	CategoryID     uuid.UUID                           `json:"category_id" validate:"required"`
	BrandID        uuid.UUID                           `json:"brand_id" validate:"required"`
	Name           string                              `json:"name" validate:"required,min=1,max=200"`
	Description    string                              `json:"description" validate:"required,max=100000"`
	IsActive       bool                                `json:"is_active" validate:"omitempty"`
	Tags           []string                            `json:"tags" validate:"required,dive,min=1,max=100"`
	ResourceIDs    []uuid.UUID                         `json:"resource_ids" validate:"omitempty,dive"`
	Specifications []catalogmodel.ProductSpecification `json:"specifications" validate:"omitempty,dive"`
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
		Account:        claims.Account,
		CategoryID:     req.CategoryID,
		BrandID:        req.BrandID,
		Name:           req.Name,
		Description:    req.Description,
		IsActive:       req.IsActive,
		Tags:           req.Tags,
		ResourceIDs:    req.ResourceIDs,
		Specifications: req.Specifications,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, spu)
}

type UpdateProductSpuRequest struct {
	ID             uuid.UUID                           `json:"id" validate:"required"`
	CategoryID     uuid.NullUUID                       `json:"category_id" validate:"omitnil"`
	FeaturedSkuID  uuid.NullUUID                       `json:"featured_sku_id" validate:"omitnil"`
	BrandID        uuid.NullUUID                       `json:"brand_id" validate:"omitnil"`
	Name           null.String                         `json:"name" validate:"omitnil,min=1,max=200"`
	Description    null.String                         `json:"description" validate:"omitnil,max=10000"`
	IsActive       null.Bool                           `json:"is_active" validate:"omitnil"`
	Tags           []string                            `json:"tags" validate:"omitempty,dive,min=1,max=100"`
	ResourceIDs    []uuid.UUID                         `json:"resource_ids" validate:"omitempty,dive"`
	Specifications []catalogmodel.ProductSpecification `json:"specifications" validate:"omitempty,dive"`
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
	ID uuid.UUID `param:"id" validate:"required"`
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

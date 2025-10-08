package catalogecho

import (
	"net/http"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSkuRequest struct {
	sharedmodel.PaginationParams
	SpuID []int64 `query:"spu_id" comma_separated:"true" validate:"required"`
	Price []int64 `query:"price" comma_separated:"true" validate:"required"`
}

func (h *Handler) ListProductSku(c echo.Context) error {
	var req ListProductSkuRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListProductSku(c.Request().Context(), catalogbiz.ListProductSkuParams{
		PaginationParams: req.PaginationParams,
		SpuID:            req.SpuID,
		Price:            req.Price,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CreateProductSkuRequest struct {
	SpuID      int64                           `validate:"required,gt=0"`
	Price      int64                           `validate:"required,gt=0"`
	CanCombine bool                            `validate:"required"`
	Attributes []catalogmodel.ProductAttribute `validate:"omitempty,dive"`
}

func (h *Handler) CreateProductSku(c echo.Context) error {
	var req CreateProductSkuRequest
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

	result, err := h.biz.CreateProductSku(c.Request().Context(), catalogbiz.CreateProductSkuParams{
		Account:    claims.Account,
		SpuID:      req.SpuID,
		Price:      req.Price,
		CanCombine: req.CanCombine,
		Attributes: req.Attributes,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateProductSkuRequest struct {
	ID         int64                           `validate:"required,gt=0"`
	Price      null.Int64                      `validate:"omitnil,gt=0"`
	CanCombine null.Bool                       `validate:"omitnil"`
	Attributes []catalogmodel.ProductAttribute `validate:"omitempty,dive"`
}

func (h *Handler) UpdateProductSku(c echo.Context) error {
	var req UpdateProductSkuRequest
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

	result, err := h.biz.UpdateProductSku(c.Request().Context(), catalogbiz.UpdateProductSkuParams{
		Account:    claims.Account,
		ID:         req.ID,
		Price:      req.Price,
		CanCombine: req.CanCombine,
		Attributes: req.Attributes,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type DeleteProductSkuRequest struct {
	ID int64 `validate:"required,gt=0"`
}

func (h *Handler) DeleteProductSku(c echo.Context) error {
	var req DeleteProductSkuRequest
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

	if err := h.biz.DeleteProductSku(c.Request().Context(), catalogbiz.DeleteProductSkuParams{
		Account: claims.Account,
		ID:      req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Delete product sku successfully")
}

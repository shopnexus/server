package catalogecho

import (
	"net/http"

	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/module/shared/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListProductSkuRequest struct {
	SpuID      int64      `query:"spu_id" validate:"omitempty,gt=0"`
	PriceFrom  null.Int64 `query:"price_from" validate:"omitnil,gt=0"`
	PriceTo    null.Int64 `query:"price_to" validate:"omitnil,gt=0,gtefield=PriceFrom"`
	CanCombine null.Bool  `query:"can_combine" validate:"omitnil"`
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
		SpuID:      req.SpuID,
		PriceFrom:  req.PriceFrom,
		PriceTo:    req.PriceTo,
		CanCombine: req.CanCombine,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type CreateProductSkuRequest struct {
	SpuID      int64                           `json:"spu_id" validate:"required,gt=0"`
	Price      int64                           `json:"price" validate:"required,gt=0"`
	CanCombine bool                            `json:"can_combine" validate:"omitempty"`
	Attributes []catalogmodel.ProductAttribute `json:"attributes" validate:"omitempty,dive"`
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
	ID         int64                           `json:"id" validate:"required,gt=0"`
	Price      null.Int64                      `json:"price" validate:"omitnil,gt=0"`
	CanCombine null.Bool                       `json:"can_combine" validate:"omitnil"`
	Attributes []catalogmodel.ProductAttribute `json:"attributes" validate:"omitempty,dive"`
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
	ID int64 `json:"id" validate:"required,gt=0"`
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

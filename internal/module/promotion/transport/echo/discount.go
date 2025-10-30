package catalogecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
	"shopnexus-remastered/internal/utils/slice"
	"time"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type PromotionRefRequest struct {
	RefType db.PromotionRefType `json:"ref_type" validate:"required"`
	RefID   int64               `json:"ref_id" validate:"required"`
}

type CreatePromotionRequest struct {
	Code        string                `json:"code" validate:"required"`
	Refs        []PromotionRefRequest `json:"refs" validate:"dive"`
	Title       string                `json:"title" validate:"required"`
	Description null.String           `json:"description" validate:"omitnil"`
	IsActive    bool                  `json:"is_active" validate:"required"`
	AutoApply   bool                  `json:"auto_apply" validate:"required"`
	DateStarted time.Time             `json:"date_started" validate:"required"`
	DateEnded   null.Time             `json:"date_ended" validate:"omitnil"`
}

type CreateDiscountRequest struct {
	CreatePromotionRequest `json:",inline"`
	MinSpend               int64 `json:"min_spend" validate:"required"`
	MaxDiscount            int64 `json:"max_discount" validate:"required"`

	// TODO: Either DiscountPercent or DiscountPrice must be provided
	DiscountPercent null.Int32 `json:"discount_percent" validate:"omitnil"`
	DiscountPrice   null.Int64 `json:"discount_price" validate:"omitnil"`
}

func (h *Handler) CreateDiscount(c echo.Context) error {
	var req CreateDiscountRequest
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

	result, err := h.biz.CreateDiscount(c.Request().Context(), promotionbiz.CreateDiscountParams{
		CreatePromotionParams: promotionbiz.CreatePromotionParams{
			Account: claims.Account,
			Refs: slice.Map(req.Refs, func(r PromotionRefRequest) promotionbiz.PromotionRef {
				return promotionbiz.PromotionRef{
					RefType: r.RefType,
					RefID:   r.RefID,
				}
			}),
			Code:        req.Code,
			Type:        db.PromotionTypeDiscount,
			Title:       req.Title,
			Description: req.Description,
			IsActive:    req.IsActive,
			AutoApply:   req.AutoApply,
			DateStarted: req.DateStarted,
			DateEnded:   req.DateEnded,
		},
		MinSpend:        req.MinSpend,
		MaxDiscount:     req.MaxDiscount,
		DiscountPercent: req.DiscountPercent,
		DiscountPrice:   req.DiscountPrice,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateDiscountRequest struct {
	UpdatePromotionRequest `json:",inline"`
	MinSpend               null.Int64 `json:"min_spend" validate:"omitnil,min=0,max=1000000000"`
	MaxDiscount            null.Int64 `json:"max_discount" validate:"omitnil,min=0,max=1000000000"`
	DiscountPercent        null.Int32 `json:"discount_percent" validate:"omitnil,min=1,max=100"`
	DiscountPrice          null.Int64 `json:"discount_price" validate:"omitnil,min=1,max=1000000000"`
}

func (h *Handler) UpdateDiscount(c echo.Context) error {
	var req UpdateDiscountRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.UpdateDiscount(c.Request().Context(), promotionbiz.UpdateDiscountParams{
		UpdatePromotionParams: promotionbiz.UpdatePromotionParams{
			ID:      req.ID,
			Code:    req.Code,
			OwnerID: req.OwnerID,
			Refs: slice.Map(req.Refs, func(r PromotionRefRequest) promotionbiz.PromotionRef {
				return promotionbiz.PromotionRef{
					RefType: r.RefType,
					RefID:   r.RefID,
				}
			}),
			Title:         req.Title,
			Description:   req.Description,
			IsActive:      req.IsActive,
			DateStarted:   req.DateStarted,
			DateEnded:     req.DateEnded,
			NullDateEnded: req.NullDateEnded,
		},
		MinSpend:        req.MinSpend,
		MaxDiscount:     req.MaxDiscount,
		DiscountPercent: req.DiscountPercent,
		DiscountPrice:   req.DiscountPrice,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

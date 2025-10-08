package catalogecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
	"time"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type CreatePromotionRequest struct {
	Code        string              `json:"code" validate:"required"`
	OwnerID     null.Int64          `json:"owner_id" validate:"required"`
	RefType     db.PromotionRefType `json:"ref_type" validate:"required"`
	RefID       null.Int64          `json:"ref_id" validate:"omitnil"`
	Type        db.PromotionType    `json:"type" validate:"required"`
	Title       string              `json:"title" validate:"required"`
	Description null.String         `json:"description" validate:"omitnil"`
	IsActive    bool                `json:"is_active" validate:"required"`
	DateStarted time.Time           `json:"date_started" validate:"required"`
	DateEnded   null.Time           `json:"date_ended" validate:"omitnil"`
}

type CreateDiscountRequest struct {
	CreatePromotionRequest
	OrderWide   bool  `json:"order_wide" validate:"required"`
	MinSpend    int64 `json:"min_spend" validate:"required"`
	MaxDiscount int64 `json:"max_discount" validate:"required"`

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
			Account:     claims.Account,
			Code:        req.Code,
			OwnerID:     req.OwnerID,
			RefType:     req.RefType,
			RefID:       req.RefID,
			Type:        req.Type,
			Title:       req.Title,
			Description: req.Description,
			IsActive:    req.IsActive,
			DateStarted: req.DateStarted,
			DateEnded:   req.DateEnded,
		},
		OrderWide:       req.OrderWide,
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
	UpdatePromotionRequest
	OrderWide       null.Bool  `json:"order_wide" validate:"omitnil"`
	MinSpend        null.Int64 `json:"min_spend" validate:"omitnil,min=0,max=1000000000"`
	MaxDiscount     null.Int64 `json:"max_discount" validate:"omitnil,min=0,max=1000000000"`
	DiscountPercent null.Int32 `json:"discount_percent" validate:"omitnil,min=1,max=100"`
	DiscountPrice   null.Int64 `json:"discount_price" validate:"omitnil,min=1,max=1000000000"`
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
			ID:            req.ID,
			Code:          req.Code,
			OwnerID:       req.OwnerID,
			RefType:       req.RefType,
			RefID:         req.RefID,
			Title:         req.Title,
			Description:   req.Description,
			IsActive:      req.IsActive,
			DateStarted:   req.DateStarted,
			DateEnded:     req.DateEnded,
			NullDateEnded: req.NullDateEnded,
		},
		OrderWide:       req.OrderWide,
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

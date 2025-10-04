package catalogecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	authbiz "shopnexus-remastered/internal/module/auth/biz"
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
	RefID       null.Int64          `json:"ref_id" validate:"required"`
	Type        db.PromotionType    `json:"type" validate:"required"`
	Title       string              `json:"title" validate:"required"`
	Description null.String         `json:"description" validate:"required"`
	IsActive    bool                `json:"is_active" validate:"required"`
	DateStarted time.Time           `json:"date_started" validate:"required"`
	DateEnded   null.Time           `json:"date_ended" validate:"required"`
}

type CreateDiscountRequest struct {
	CreatePromotionRequest
	OrderWide   bool  `json:"order_wide" validate:"required"`
	MinSpend    int64 `json:"min_spend" validate:"required"`
	MaxDiscount int64 `json:"max_discount" validate:"required"`

	// TODO: Either DiscountPercent or DiscountPrice must be provided
	DiscountPercent null.Int32 `json:"discount_percent" validate:"required"`
	DiscountPrice   null.Int64 `json:"discount_price" validate:"required"`
}

func (h *Handler) CreateDiscount(c echo.Context) error {
	var req CreateDiscountRequest
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

type UpdatePromotionRequest struct {
	ID               int64               `json:"id" validate:"required,gt=0"`
	Code             null.String         `json:"code" validate:"required,alphanum,min=3,max=50"`
	OwnerID          null.Int64          `json:"owner_id" validate:"omitnil"`
	RefType          db.PromotionRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	RefID            null.Int64          `json:"ref_id" validate:"omitnil"`
	Type             db.PromotionType    `json:"type" validate:"required,validateFn=Valid"`
	Title            string              `json:"title" validate:"required,min=3,max=200"`
	Description      null.String         `json:"description" validate:"omitnil,max=1000"`
	IsActive         bool                `json:"is_active" validate:"required"`
	DateStarted      time.Time           `json:"date_started" validate:"required"`
	DateEnded        null.Time           `json:"date_ended" validate:"omitnil,gtfield=DateStarted"`
	ScheduleTz       null.String         `json:"schedule_tz" validate:"omitnil,timezone"`
	ScheduleStart    null.Time           `json:"schedule_start" validate:"omitnil"`
	ScheduleDuration null.Int32          `json:"schedule_duration" validate:"omitnil,gte=0,lte=1440"`
}

func (h *Handler) UpdatePromotion(c echo.Context) error {
	var req UpdatePromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	//
	//result, err := h.biz.UpdatePromotion(c.Request().Context(), promotionbiz.UpdatePromotionParams{
	//	ID:               req.ID,
	//	Code:             req.Code,
	//	OwnerID:          req.OwnerID,
	//	RefType:          req.RefType,
	//	RefID:            req.RefID,
	//	Type:             req.Type,
	//	Title:            req.Title,
	//	Description:      req.Description,
	//	IsActive:         req.IsActive,
	//	DateStarted:      req.DateStarted,
	//	DateEnded:        req.DateEnded,
	//	ScheduleTz:       req.ScheduleTz,
	//	ScheduleStart:    req.ScheduleStart,
	//	ScheduleDuration: req.ScheduleDuration,
	//})
	//if err != nil {
	//	return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	//}

	return response.FromDTO(c.Response().Writer, http.StatusOK, nil)
}

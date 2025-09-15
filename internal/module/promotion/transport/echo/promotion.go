package catalogecho

import (
	"net/http"
	"time"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"

	"shopnexus-remastered/internal/db"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
)

type Handler struct {
	biz *promotionbiz.PromotionBiz
}

func NewHandler(e *echo.Echo, biz *promotionbiz.PromotionBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/catalog")
	_ = api

	return h
}

type GetPromotionRequest struct {
	ID int64 `query:"id" validate:"required"`
}

func (h *Handler) GetPromotion(c echo.Context) error {
	var req GetPromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.GetPromotion(c.Request().Context(), promotionbiz.GetPromotionParams{
		ID: req.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListPromotionRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListPromotion(c echo.Context) error {
	var req ListPromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListPromotion(c.Request().Context(), promotionbiz.ListPromotionParams{
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CreatePromotionRequest struct {
	Code             string              `json:"code" validate:"required"`
	OwnerID          null.Int64          `json:"owner_id" validate:"required"`
	RefType          db.PromotionRefType `json:"ref_type" validate:"required"`
	RefID            null.Int64          `json:"ref_id" validate:"required"`
	Type             db.PromotionType    `json:"type" validate:"required"`
	Title            string              `json:"title" validate:"required"`
	Description      null.String         `json:"description" validate:"required"`
	IsActive         bool                `json:"is_active" validate:"required"`
	DateStarted      time.Time           `json:"date_started" validate:"required"`
	DateEnded        null.Time           `json:"date_ended" validate:"required"`
	ScheduleTz       null.String         `json:"schedule_tz" validate:"required"`
	ScheduleStart    null.Time           `json:"schedule_start" validate:"required"`
	ScheduleDuration null.Int32          `json:"schedule_duration" validate:"required"`
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

	result, err := h.biz.CreateDiscount(c.Request().Context(), promotionbiz.CreateDiscountParams{
		CreatePromotionParams: promotionbiz.CreatePromotionParams{
			Code:             req.Code,
			OwnerID:          req.OwnerID,
			RefType:          req.RefType,
			RefID:            req.RefID,
			Type:             req.Type,
			Title:            req.Title,
			Description:      req.Description,
			IsActive:         req.IsActive,
			DateStarted:      req.DateStarted,
			DateEnded:        req.DateEnded,
			ScheduleTz:       req.ScheduleTz,
			ScheduleStart:    req.ScheduleStart,
			ScheduleDuration: req.ScheduleDuration,
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

type DeletePromotionRequest struct {
	ID int64 `query:"id" validate:"required,gt=0"`
}

func (h *Handler) DeletePromotion(c echo.Context) error {
	var req DeletePromotionRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	err := h.biz.DeletePromotion(c.Request().Context(), promotionbiz.DeletePromotionParams{
		ID: req.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Promotion deleted successfully")
}

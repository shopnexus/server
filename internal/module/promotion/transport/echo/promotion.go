package catalogecho

import (
	"net/http"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"

	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/module/shared/response"
)

type Handler struct {
	biz *promotionbiz.PromotionBiz
}

func NewHandler(e *echo.Echo, biz *promotionbiz.PromotionBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/catalog")
	_ = api

	promotionApi := api.Group("/promotion")
	promotionApi.GET("/:id", h.GetPromotion)
	promotionApi.GET("", h.ListPromotion)
	promotionApi.DELETE("/:id", h.DeletePromotion)
	promotionApi.PATCH("/discount", h.UpdateDiscount)
	promotionApi.POST("/discount", h.CreateDiscount)

	return h
}

type GetPromotionRequest struct {
	ID int64 `param:"id" validate:"required"`
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
	commonmodel.PaginationParams
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

type DeletePromotionRequest struct {
	ID int64 `param:"id" validate:"required,gt=0"`
}

func (h *Handler) DeletePromotion(c echo.Context) error {
	var req DeletePromotionRequest
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

	if err = h.biz.DeletePromotion(c.Request().Context(), promotionbiz.DeletePromotionParams{
		Account: claims.Account,
		ID:      req.ID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Promotion deleted successfully")
}

type UpdatePromotionRequest struct {
	ID            int64                 `json:"id" validate:"required"`
	Code          null.String           `json:"code" validate:"omitnil"`
	OwnerID       null.Int64            `json:"owner_id" validate:"omitnil"`
	Refs          []PromotionRefRequest `json:"refs" validate:"dive"`
	Title         null.String           `json:"title" validate:"omitnil"`
	Description   null.String           `json:"description" validate:"omitnil"`
	IsActive      null.Bool             `json:"is_active" validate:"omitnil"`
	DateStarted   null.Time             `json:"date_started" validate:"omitnil"`
	DateEnded     null.Time             `json:"date_ended" validate:"omitnil"`
	NullDateEnded bool                  `json:"null_date_ended" validate:"omitempty"`
}

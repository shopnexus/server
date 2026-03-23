package accountecho

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	authclaims "shopnexus-server/internal/shared/claims"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/response"
)

type Handler struct {
	biz analyticbiz.AnalyticClient
}

func NewHandler(e *echo.Echo, biz analyticbiz.AnalyticClient) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/analytic")
	api.POST("/interaction", h.CreateInteraction)
	api.GET("/popularity/top", h.ListTopProductPopularity)
	api.GET("/popularity/:spu_id", h.GetProductPopularity)

	return h
}

type CreateInteraction struct {
	EventType string                                `json:"event_type" validate:"required,min=1"`
	RefType   analyticdb.AnalyticInteractionRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	RefID     string                                `json:"ref_id" validate:"required"`
}

type CreateInteractionRequest struct {
	Interactions []CreateInteraction `json:"interactions" validate:"required,dive,required"`
}

func (h *Handler) CreateInteraction(c echo.Context) error {
	var req CreateInteractionRequest
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

	if err := h.biz.CreateInteraction(c.Request().Context(), analyticbiz.CreateInteractionParams{
		Interactions: lo.Map(req.Interactions, func(i CreateInteraction, _ int) analyticbiz.CreateInteraction {
			return analyticbiz.CreateInteraction{
				Account:   claims.Account,
				EventType: i.EventType,
				RefType:   i.RefType,
				RefID:     i.RefID,
			}
		}),
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Interaction created successfully")
}

type GetProductPopularityRequest struct {
	SpuID uuid.UUID `param:"spu_id" validate:"required"`
}

func (h *Handler) GetProductPopularity(c echo.Context) error {
	var req GetProductPopularityRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.GetProductPopularity(c.Request().Context(), req.SpuID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListTopProductPopularityRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListTopProductPopularity(c echo.Context) error {
	var req ListTopProductPopularityRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListTopProductPopularity(c.Request().Context(), req.PaginationParams)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

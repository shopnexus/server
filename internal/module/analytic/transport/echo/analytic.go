package accountecho

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	authclaims "shopnexus-remastered/internal/shared/claims"
	"shopnexus-remastered/internal/shared/response"
)

type Handler struct {
	biz *analyticbiz.AnalyticBiz
}

func NewHandler(e *echo.Echo, biz *analyticbiz.AnalyticBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/analytic")
	api.POST("/interaction", h.CreateInteraction)

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
				AccountID: claims.Account.ID,
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

package accountecho

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"shopnexus-remastered/internal/db"
	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"
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

type CreateInteractionRequest struct {
	EventType string                        `json:"event_type" validate:"required,min=1"`
	RefType   db.AnalyticInteractionRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	RefID     int64                         `json:"ref_id" validate:"required,min=1"`
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
		Account:   claims.Account,
		EventType: req.EventType,
		RefType:   req.RefType,
		RefID:     req.RefID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Interaction created successfully")
}

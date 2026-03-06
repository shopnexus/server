package chatecho

import (
	"net/http"
	"sync"

	chatbiz "shopnexus-remastered/internal/module/chat/biz"
	authclaims "shopnexus-remastered/internal/shared/claims"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz       *chatbiz.ChatBiz
	upgrader  websocket.Upgrader
	clients   map[uuid.UUID]*websocket.Conn
	clientsMu sync.RWMutex
}

func NewHandler(e *echo.Echo, biz *chatbiz.ChatBiz) *Handler {
	h := &Handler{
		biz: biz,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[uuid.UUID]*websocket.Conn),
	}

	api := e.Group("/api/v1/chat")
	api.POST("/conversation", h.CreateConversation)
	api.GET("/conversation", h.ListConversation)
	api.GET("/conversation/:id/messages", h.ListMessage)

	e.GET("/ws/chat", h.HandleWebSocket)

	return h
}

type CreateConversationRequest struct {
	VendorID uuid.UUID `json:"vendor_id" validate:"required"`
}

func (h *Handler) CreateConversation(c echo.Context) error {
	var req CreateConversationRequest
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

	result, err := h.biz.CreateConversation(c.Request().Context(), chatbiz.CreateConversationParams{
		Account:  claims.Account,
		VendorID: req.VendorID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ListConversationRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListConversation(c echo.Context) error {
	var req ListConversationRequest
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

	result, err := h.biz.ListConversation(c.Request().Context(), chatbiz.ListConversationParams{
		Account:          claims.Account,
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type ListMessageRequest struct {
	ConversationID uuid.UUID `param:"id" validate:"required"`
	sharedmodel.PaginationParams
}

func (h *Handler) ListMessage(c echo.Context) error {
	var req ListMessageRequest
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

	result, err := h.biz.ListMessage(c.Request().Context(), chatbiz.ListMessageParams{
		Account:          claims.Account,
		ConversationID:   req.ConversationID,
		PaginationParams: req.PaginationParams,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

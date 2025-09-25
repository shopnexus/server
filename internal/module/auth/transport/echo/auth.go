package echo

import (
	"net/http"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/module/auth/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *authbiz.AuthBiz
}

func NewHandler(e *echo.Echo, authbiz *authbiz.AuthBiz) *Handler {
	h := &Handler{biz: authbiz}
	api := e.Group("/api/v1/auth")
	api.POST("/login/basic", h.LoginBasic)
	api.POST("/register/basic", h.RegisterBasic)
	api.POST("/refresh", h.Refresh)

	return h
}

type LoginBasicRequest struct {
	ID       string `json:"id" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type LoginBasicResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) LoginBasic(c echo.Context) error {
	var req LoginBasicRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.Login(c.Request().Context(), authbiz.LoginParams{
		Username: null.NewString(req.ID, true),
		Email:    null.NewString(req.ID, true),
		Phone:    null.NewString(req.ID, true),
		Password: null.NewString(req.Password, true),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, LoginBasicResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

type RegisterBasicRequest struct {
	Type     db.AccountType `json:"type" validate:"required"`
	Username null.String    `json:"username" validate:"omitnil"`
	Email    null.String    `json:"email" validate:"omitnil"`
	Phone    null.String    `json:"phone" validate:"omitnil"`
	Password string         `json:"password" validate:"required"`
}

type RegisterBasicResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) RegisterBasic(c echo.Context) error {
	var req RegisterBasicRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.Register(c.Request().Context(), authbiz.RegisterParams{
		Type:     req.Type,
		Username: req.Username,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: null.NewString(req.Password, true),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusCreated, RegisterBasicResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(c echo.Context) error {
	var req RefreshRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.Refresh(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, RefreshResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

package accountecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	authclaims "shopnexus-remastered/internal/module/auth/biz/claims"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *accountbiz.AccountBiz
}

func NewHandler(e *echo.Echo, biz *accountbiz.AccountBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/account")

	// Account endpoints)
	api.GET("", h.GetAccount)
	api.PATCH("", h.UpdateAccount)

	// Me endpoints
	meApi := api.Group("/me")
	meApi.GET("", h.GetMe)
	meApi.PATCH("", h.UpdateMe)

	// Cart endpoints
	cartApi := api.Group("/cart")
	cartApi.GET("", h.GetCart)
	cartApi.POST("", h.UpdateCart)
	cartApi.DELETE("", h.ClearCart)

	return h
}

type GetAccountParams struct {
	AccountID int64
}

func (h *Handler) GetAccount(c echo.Context) error {
	var req GetAccountParams
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

	profile, err := h.biz.GetProfile(c.Request().Context(), accountbiz.GetProfileParams{
		Issuer:    claims.Account,
		AccountID: req.AccountID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, profile)
}

type UpdateAccountRequest struct {
	// Account base fields
	AccountID int64            `json:"account_id" validate:"required"`
	Status    db.AccountStatus `json:"status" validate:"omitempty,validateFn=Valid"`
	Username  null.String      `json:"username" validate:"omitempty,min=3,max=30,alphanum"`
	Phone     null.String      `json:"phone" validate:"omitempty,e164"`
	Email     null.String      `json:"email" validate:"omitempty,email"`

	// Profile fields
	Gender           db.AccountGender `json:"gender" validate:"omitempty,validateFn=Valid"`
	Name             null.String      `json:"name" validate:"omitnil"`
	DateOfBirth      null.Time        `json:"date_of_birth" validate:"omitnil"`
	AvatarRsID       null.Int64       `json:"avatar_rs_id" validate:"omitnil"`
	DefaultContactID null.Int64       `json:"default_contact_id" validate:"omitnil"`

	// Vendor fields
	Description null.String `json:"description" validate:"omitnil,max=500"`
}

func (h *Handler) UpdateAccount(c echo.Context) error {
	var req UpdateAccountRequest
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

	result, err := h.biz.UpdateProfile(c.Request().Context(), accountbiz.UpdateProfileParams{
		Issuer:           claims.Account,
		AccountID:        req.AccountID,
		Status:           req.Status,
		Username:         req.Username,
		Phone:            req.Phone,
		Email:            req.Email,
		Gender:           req.Gender,
		Name:             req.Name,
		DateOfBirth:      req.DateOfBirth,
		AvatarRsID:       req.AvatarRsID,
		DefaultContactID: req.DefaultContactID,
		Description:      req.Description,
	})

	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

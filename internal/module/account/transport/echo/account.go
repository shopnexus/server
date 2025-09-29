package accountecho

import (
	"net/http"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	authbiz "shopnexus-remastered/internal/module/auth/biz"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *accountbiz.AccountBiz
}

func NewHandler(e *echo.Echo, biz *accountbiz.AccountBiz) *Handler {
	h := &Handler{biz: biz}
	api := e.Group("/api/v1/account")
	//api.GET("/", h.GetAccount)
	api.GET("/me", h.GetMe)

	// Cart endpoints
	cartApi := api.Group("/cart")
	cartApi.GET("", h.GetCart)
	cartApi.POST("", h.UpdateCart)
	cartApi.DELETE("", h.ClearCart)

	return h
}

func (h *Handler) GetMe(c echo.Context) error {
	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	profile, err := h.biz.GetProfile(c.Request().Context(), accountbiz.GetProfileParams{
		AccountID: claims.Account.ID,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, profile)
}

//type GetProfileRequest struct {
//	AccountID int64 `query:"account_id" validate:"required"`
//}
//
//func (h *Handler) GetProfile(c echo.Context) error {
//	var req GetProfileRequest
//	if err := c.Bind(&req); err != nil {
//		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
//	}
//	if err := c.Validate(&req); err != nil {
//		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
//	}
//
//	//claims, err := authbiz.GetClaims(c.Request())
//	//if err != nil {
//	//	return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
//	//}
//
//	result, err := h.biz.GetProfile(c.Request().Context(), accountbiz.GetProfileParams{
//		AccountID: req.AccountID,
//	})
//	if err != nil {
//		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
//	}
//
//	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
//}

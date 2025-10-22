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

type ListContactRequest struct {
}

func (h *Handler) ListContact(c echo.Context) error {
	var req ListContactRequest
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

	result, err := h.biz.ListContact(c.Request().Context(), accountbiz.ListContactParams{
		Account: claims.Account,
	})

	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type GetContactRequest struct {
	ContactID int64 `param:"contact_id" validate:"required"`
}

func (h *Handler) GetContact(c echo.Context) error {
	var req GetContactRequest
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

	result, err := h.biz.GetContact(c.Request().Context(), accountbiz.GetContactParams{
		Account:   claims.Account,
		ContactID: req.ContactID,
	})

	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type CreateContactRequest struct {
	FullName    string                `json:"full_name" validate:"required"`
	Phone       string                `json:"phone" validate:"required"`
	Address     string                `json:"address" validate:"required"`
	AddressType db.AccountAddressType `json:"address_type" validate:"required,validateFn=Valid"`
}

func (h *Handler) CreateContact(c echo.Context) error {
	var req CreateContactRequest
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

	result, err := h.biz.CreateContact(c.Request().Context(), accountbiz.CreateContactParams{
		Account:     claims.Account,
		FullName:    req.FullName,
		Phone:       req.Phone,
		Address:     req.Address,
		AddressType: req.AddressType,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateContactRequest struct {
	ContactID     int64                 `json:"contact_id" validate:"required"`
	FullName      null.String           `json:"full_name" validate:"omitnil"`
	Phone         null.String           `json:"phone" validate:"omitnil"`
	Address       null.String           `json:"address" validate:"omitnil"`
	AddressType   db.AccountAddressType `json:"address_type" validate:"omitempty,validateFn=Valid"`
	PhoneVerified null.Bool             `json:"phone_verified" validate:"omitnil"`
}

func (h *Handler) UpdateContact(c echo.Context) error {
	var req UpdateContactRequest
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

	result, err := h.biz.UpdateContact(c.Request().Context(), accountbiz.UpdateContactParams{
		Account:       claims.Account,
		ContactID:     req.ContactID,
		FullName:      req.FullName,
		Phone:         req.Phone,
		Address:       req.Address,
		AddressType:   req.AddressType,
		PhoneVerified: req.PhoneVerified,
	})

	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type DeleteContactRequest struct {
	ContactID int64 `json:"contact_id" validate:"required"`
}

func (h *Handler) DeleteContact(c echo.Context) error {
	var req DeleteContactRequest
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

	if err := h.biz.DeleteContact(c.Request().Context(), accountbiz.DeleteContactParams{
		Account:   claims.Account,
		ContactID: req.ContactID,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "Delete contact successfully")
}

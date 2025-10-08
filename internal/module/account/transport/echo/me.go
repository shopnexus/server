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

func (h *Handler) GetMe(c echo.Context) error {
	claims, err := authclaims.GetClaims(c.Request())
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

type UpdateMeRequest struct {
	// Account base fields
	Status   null.Value[db.AccountStatus] `json:"status" validate:"omitempty,validFn=Valid"`
	Username null.String                  `json:"username" validate:"omitempty,min=3,max=30,alphanum"`
	Phone    null.String                  `json:"phone" validate:"omitempty,e164"`
	Email    null.String                  `json:"email" validate:"omitempty,email"`

	// Profile fields
	Gender           null.Value[db.AccountGender] `json:"gender" validate:"omitnil,validFn=Valid"`
	Name             null.String                  `json:"name" validate:"omitnil"`
	DateOfBirth      null.Time                    `json:"date_of_birth" validate:"omitnil"`
	AvatarRsID       null.Int64                   `json:"avatar_rs_id" validate:"omitnil"`
	DefaultContactID null.Int64                   `json:"default_contact_id" validate:"omitnil"`

	// Vendor fields
	Description null.String `json:"description" validate:"omitnil,max=500"`
}

func (h *Handler) UpdateMe(c echo.Context) error {
	var req UpdateMeRequest
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
		AccountID:        claims.Account.ID,
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

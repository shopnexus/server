package catalogecho

import (
	"net/http"

	"shopnexus-remastered/internal/db"
	authbiz "shopnexus-remastered/internal/module/auth/biz"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListCommentRequest struct {
	sharedmodel.PaginationParams
	RefType   db.CatalogCommentRefType `query:"ref_type" validate:"required"`
	ID        []int64                  `query:"id" validate:"omitempty"`
	AccountID []int64                  `query:"account_id" validate:"omitempty"`
	RefID     []int64                  `query:"ref_id" validate:"omitempty"`
	ScoreFrom null.Int32               `query:"score_from" validate:"omitnil"`
	ScoreTo   null.Int32               `query:"score_to" validate:"omitnil"`
}

func (h *Handler) ListComment(c echo.Context) error {
	var req ListCommentRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.biz.ListComment(c.Request().Context(), catalogbiz.ListCommentParams{
		PaginationParams: req.PaginationParams,
		RefType:          req.RefType,
		ID:               req.ID,
		AccountID:        req.AccountID,
		RefID:            req.RefID,
		ScoreFrom:        req.ScoreFrom,
		ScoreTo:          req.ScoreTo,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CreateCommentRequest struct {
	RefType db.CatalogCommentRefType `json:"ref_type" validate:"required"`
	RefID   int64                    `json:"ref_id" validate:"required"`
	Body    string                   `json:"body" validate:"required"`
	Score   int32                    `json:"score" validate:"required"`

	Resources []sharedmodel.CreateResource `json:"resources" validate:"required"`
}

func (h *Handler) CreateComment(c echo.Context) error {
	var req CreateCommentRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.CreateComment(c.Request().Context(), catalogbiz.CreateCommentParams{
		Account: claims.Account,
		RefType: req.RefType,
		RefID:   req.RefID,
		Body:    req.Body,
		Score:   req.Score,

		Resources: req.Resources,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateCommentRequest struct {
	Body           null.String                  `json:"body" validate:"required"`
	Score          null.Int32                   `json:"score" validate:"required"`
	Resources      []sharedmodel.CreateResource `json:"resources" validate:"required"`
	EmptyResources bool                         `json:"empty_resources" validate:"required"`
}

func (h *Handler) UpdateComment(c echo.Context) error {
	var req UpdateCommentRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	comment, err := h.biz.UpdateComment(c.Request().Context(), catalogbiz.UpdateCommentParams{
		Account:   claims.Account,
		ID:        claims.Account.ID,
		Body:      req.Body,
		Score:     req.Score,
		Resources: req.Resources,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, comment)
}

type DeleteCommentRequest struct {
	IDs []int64 `json:"ids" validate:"required"`
}

func (h *Handler) DeleteComment(c echo.Context) error {
	var req DeleteCommentRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authbiz.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	if err := h.biz.DeleteComment(c.Request().Context(), catalogbiz.DeleteCommentParams{
		Account: claims.Account,
		IDs:     req.IDs,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "comment deleted")
}

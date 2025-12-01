package catalogecho

import (
	"net/http"

	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogdb "shopnexus-remastered/internal/module/catalog/db"
	authclaims "shopnexus-remastered/internal/shared/claims"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/response"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/labstack/echo/v4"
)

type ListCommentRequest struct {
	commonmodel.PaginationParams
	RefType   catalogdb.CatalogCommentRefType `query:"ref_type" validate:"required"`
	RefID     uuid.UUID                       `query:"ref_id" validate:"required"`
	ID        []uuid.UUID                     `query:"id" validate:"omitempty"`
	AccountID []uuid.UUID                     `query:"account_id" validate:"omitempty"`
	ScoreFrom null.Float                      `query:"score_from" validate:"omitnil"`
	ScoreTo   null.Float                      `query:"score_to" validate:"omitnil"`
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
		RefID:            []uuid.UUID{req.RefID},
		ScoreFrom:        req.ScoreFrom,
		ScoreTo:          req.ScoreTo,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromPaginate(c.Response().Writer, result)
}

type CreateCommentRequest struct {
	RefType catalogdb.CatalogCommentRefType `json:"ref_type" validate:"required,validateFn=Valid"`
	RefID   uuid.UUID                       `json:"ref_id" validate:"required"`
	Body    string                          `json:"body" validate:"required"`
	Score   float64                         `json:"score" validate:"required"`

	ResourceIDs []uuid.UUID `json:"resource_ids" validate:"required"`
}

func (h *Handler) CreateComment(c echo.Context) error {
	var req CreateCommentRequest
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

	result, err := h.biz.CreateComment(c.Request().Context(), catalogbiz.CreateCommentParams{
		Account: claims.Account,
		RefType: req.RefType,
		RefID:   req.RefID,
		Body:    req.Body,
		Score:   req.Score,

		ResourceIDs: req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type UpdateCommentRequest struct {
	ID             uuid.UUID   `json:"id" validate:"required"`
	Body           null.String `json:"body" validate:"required"`
	Score          null.Float  `json:"score" validate:"required"`
	ResourceIDs    []uuid.UUID `json:"resource_ids" validate:"required"`
	EmptyResources bool        `json:"empty_resources" validate:"omitempty"`
}

func (h *Handler) UpdateComment(c echo.Context) error {
	var req UpdateCommentRequest
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

	comment, err := h.biz.UpdateComment(c.Request().Context(), catalogbiz.UpdateCommentParams{
		Account:     claims.Account,
		ID:          req.ID,
		Body:        req.Body,
		Score:       req.Score,
		ResourceIDs: req.ResourceIDs,
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, comment)
}

type DeleteCommentRequest struct {
	IDs []uuid.UUID `json:"ids" validate:"required"`
}

func (h *Handler) DeleteComment(c echo.Context) error {
	var req DeleteCommentRequest
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

	if err := h.biz.DeleteComment(c.Request().Context(), catalogbiz.DeleteCommentParams{
		Account:    claims.Account,
		CommentIDs: req.IDs,
	}); err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromMessage(c.Response().Writer, http.StatusOK, "comment deleted")
}

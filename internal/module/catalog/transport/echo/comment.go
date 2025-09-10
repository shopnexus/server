package catalogecho

import (
	"net/http"
	"shopnexus-remastered/internal/db"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/response"

	"github.com/labstack/echo/v4"
)

type ListCommentRequest struct {
	sharedmodel.PaginationParams
	RefType   db.CatalogCommentRefType `query:"ref_type" validate:"required"`
	ID        []int64                  `query:"id" validate:"omitempty,dive,gt=0"`
	AccountID []int64                  `query:"account_id" validate:"omitempty,dive,gt=0"`
	RefID     []int64                  `query:"ref_id" validate:"omitempty,dive,gt=0"`
	ScoreFrom *int32                   `query:"score_from" validate:"omitempty,gte=1,lte=10"`
	ScoreTo   *int32                   `query:"score_to" validate:"omitempty,gte=1,lte=10"`
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

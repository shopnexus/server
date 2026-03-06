package catalogbiz

import (
	"context"
	"fmt"

	accountbiz "shopnexus-remastered/internal/module/account/biz"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListCommentParams struct {
	sharedmodel.PaginationParams
	Account   accountmodel.AuthenticatedAccount
	RefType   catalogdb.CatalogCommentRefType `validate:"required,validateFn=Valid"`
	ID        []uuid.UUID                     `validate:"omitempty,dive,gt=0"`
	AccountID []uuid.UUID                     `validate:"omitempty,dive,gt=0"`
	RefID     []uuid.UUID                     `validate:"omitempty,dive,gt=0"`
	ScoreFrom null.Float                      `validate:"omitnil,gte=0,lte=1"`
	ScoreTo   null.Float                      `validate:"omitnil,gte=0,lte=1"`
}

func (b *CatalogBiz) ListComment(ctx context.Context, params ListCommentParams) (sharedmodel.PaginateResult[catalogmodel.Comment], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.Comment]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listComment, err := b.storage.Querier().ListCountComment(ctx, catalogdb.ListCountCommentParams{
		Limit:     params.Limit,
		Offset:    params.Offset(),
		ID:        params.ID,
		AccountID: params.AccountID,
		RefType:   []catalogdb.CatalogCommentRefType{params.RefType},
		RefID:     params.RefID,
		ScoreFrom: params.ScoreFrom,
		ScoreTo:   params.ScoreTo,
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(listComment) > 0 {
		total.SetValid(listComment[0].TotalCount)
	}

	dbComments := lo.Map(listComment, func(row catalogdb.ListCountCommentRow, _ int) catalogdb.CatalogComment {
		return row.CatalogComment
	})

	var commentIDs []uuid.UUID
	var accountIDs []uuid.UUID
	for _, row := range dbComments {
		commentIDs = append(commentIDs, row.ID)
		accountIDs = append(accountIDs, row.AccountID)
	}

	// Map accounts to comments
	listProfile, err := b.account.ListProfile(ctx, accountbiz.ListProfileParams{
		Issuer:     params.Account,
		AccountIDs: accountIDs,
	})
	if err != nil {
		return zero, err
	}
	// map[accountID]catalogdb.AccountProfile
	profileMap := lo.KeyBy(listProfile.Data, func(a accountmodel.Profile) uuid.UUID { return a.ID })

	// Map resources to comments
	resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeComment, commentIDs)
	if err != nil {
		return zero, err
	}

	var comments []catalogmodel.Comment
	for _, dbComment := range dbComments {
		comments = append(comments, catalogmodel.Comment{
			ID:          dbComment.ID,
			Profile:     profileMap[dbComment.AccountID],
			Body:        dbComment.Body,
			Upvote:      dbComment.Upvote,
			Downvote:    dbComment.Downvote,
			Score:       dbComment.Score,
			DateCreated: dbComment.DateCreated,
			DateUpdated: dbComment.DateUpdated,
			Resources:   resourcesMap[dbComment.ID],
		})
	}

	return sharedmodel.PaginateResult[catalogmodel.Comment]{
		PageParams: params.PaginationParams,
		Data:       comments,
		Total:      total,
	}, nil
}

type CreateCommentParams struct {
	Storage CatalogStorage
	Account accountmodel.AuthenticatedAccount

	RefType catalogdb.CatalogCommentRefType `validate:"required"`
	RefID   uuid.UUID                       `validate:"required"`
	Body    string                          `validate:"required,min=1,max=1000"`
	Score   float64                         `validate:"required,gte=0,lte=1"`

	ResourceIDs []uuid.UUID `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateComment(ctx context.Context, params CreateCommentParams) (catalogmodel.Comment, error) {
	var zero catalogmodel.Comment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		comment   catalogdb.CatalogComment
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		var err error
		comment, err = txStorage.Querier().CreateDefaultComment(ctx, catalogdb.CreateDefaultCommentParams{
			AccountID: params.Account.ID,
			RefType:   params.RefType,
			RefID:     params.RefID,
			Body:      params.Body,
			Score:     params.Score,
		})
		if err != nil {
			return err
		}

		// Attach resources
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			// TODO: use message queue instead sequential calls
			Storage:     pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			Account:     params.Account,
			RefType:     commondb.CommonResourceRefTypeComment,
			RefID:       comment.ID,
			ResourceIDs: params.ResourceIDs,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create comment: %w", err)
	}

	profile, err := b.account.GetProfile(ctx, accountbiz.GetProfileParams{
		Issuer:    params.Account,
		AccountID: comment.AccountID,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to get comment profile: %w", err)
	}

	// Track analytic interactions for product reviews
	if params.RefType == catalogdb.CatalogCommentRefTypeProductSpu {
		refID := params.RefID.String()
		interactions := []analyticbiz.CreateInteraction{
			{Account: params.Account, EventType: analyticmodel.EventWriteReview, RefType: analyticdb.AnalyticInteractionRefTypeProduct, RefID: refID},
		}
		switch {
		case params.Score >= 0.8:
			interactions = append(interactions, analyticbiz.CreateInteraction{Account: params.Account, EventType: analyticmodel.EventRatingHigh, RefType: analyticdb.AnalyticInteractionRefTypeProduct, RefID: refID})
		case params.Score >= 0.4:
			interactions = append(interactions, analyticbiz.CreateInteraction{Account: params.Account, EventType: analyticmodel.EventRatingMed, RefType: analyticdb.AnalyticInteractionRefTypeProduct, RefID: refID})
		default:
			interactions = append(interactions, analyticbiz.CreateInteraction{Account: params.Account, EventType: analyticmodel.EventRatingLow, RefType: analyticdb.AnalyticInteractionRefTypeProduct, RefID: refID})
		}
		b.analytic.TrackInteractions(interactions)
	}

	return catalogmodel.Comment{
		ID:          comment.ID,
		Profile:     profile,
		Body:        comment.Body,
		Upvote:      comment.Upvote,
		Downvote:    comment.Downvote,
		Score:       comment.Score,
		DateCreated: comment.DateCreated,
		DateUpdated: comment.DateUpdated,
		Resources:   resources,
	}, nil
}

type UpdateCommentParams struct {
	Storage CatalogStorage
	Account accountmodel.AuthenticatedAccount

	ID            uuid.UUID   `validate:"required"`
	Body          null.String `validate:"omitempty,min=1,max=1000"`
	Score         null.Float  `validate:"omitempty,gte=0,lte=1"`
	UpvoteDelta   null.Int64  `validate:"omitempty,ne=0"`
	DownvoteDelta null.Int64  `validate:"omitempty,ne=0"`

	ResourceIDs    []uuid.UUID `validate:"omitempty,dive"`
	EmptyResources bool
}

func (b *CatalogBiz) UpdateComment(ctx context.Context, params UpdateCommentParams) (catalogmodel.Comment, error) {
	var zero catalogmodel.Comment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		comment   catalogdb.CatalogComment
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		var err error

		// Update base comment info
		comment, err = txStorage.Querier().UpdateComment(ctx, catalogdb.UpdateCommentParams{
			ID:    params.ID,
			Body:  params.Body,
			Score: params.Score,
		})
		if err != nil {
			return err
		}

		// Update upvote/downvote count
		if params.UpvoteDelta.Valid || params.DownvoteDelta.Valid {
			if err := txStorage.Querier().UpdateCommentUpvoteDownvote(ctx, catalogdb.UpdateCommentUpvoteDownvoteParams{
				ID:            params.ID,
				UpvoteDelta:   params.UpvoteDelta,
				DownvoteDelta: params.DownvoteDelta,
			}); err != nil {
				return err
			}
		}

		// Update resources
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeComment,
			RefID:           params.ID,
			ResourceIDs:     params.ResourceIDs,
			EmptyResources:  true, // User may want to remove all resources (set to empty)
			DeleteResources: true,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update comment: %w", err)
	}

	profile, err := b.account.GetProfile(ctx, accountbiz.GetProfileParams{
		Issuer:    params.Account,
		AccountID: comment.AccountID,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to get comment profile: %w", err)
	}

	return catalogmodel.Comment{
		ID:          comment.ID,
		Profile:     profile,
		Body:        comment.Body,
		Upvote:      comment.Upvote,
		Downvote:    comment.Downvote,
		Score:       comment.Score,
		DateCreated: comment.DateCreated,
		DateUpdated: comment.DateUpdated,
		Resources:   resources,
	}, nil
}

type DeleteCommentParams struct {
	Storage CatalogStorage
	Account accountmodel.AuthenticatedAccount

	CommentIDs []uuid.UUID `validate:"required,dive,gt=0"`
}

func (b *CatalogBiz) DeleteComment(ctx context.Context, params DeleteCommentParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		// Delete base comments
		if err := txStorage.Querier().DeleteComment(ctx, catalogdb.DeleteCommentParams{
			ID: params.CommentIDs,
		}); err != nil {
			return err
		}

		// Remove associated resources
		if err := b.common.DeleteResources(ctx, commonbiz.DeleteResourcesParams{
			// TODO: use message queue instead sequential calls
			Storage:         pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			RefType:         commondb.CommonResourceRefTypeComment,
			RefID:           params.CommentIDs,
			DeleteResources: true,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	return nil
}

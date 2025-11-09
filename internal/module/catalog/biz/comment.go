package catalogbiz

import (
	"context"
	"fmt"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
)

type ListCommentParams struct {
	commonmodel.PaginationParams
	RefType   db.CatalogCommentRefType `validate:"required"`
	ID        []int64                  `validate:"omitempty,dive,gt=0"`
	AccountID []int64                  `validate:"omitempty,dive,gt=0"`
	RefID     []int64                  `validate:"omitempty,dive,gt=0"`
	ScoreFrom null.Int32               `validate:"omitnil,gte=1,lte=10"`
	ScoreTo   null.Int32               `validate:"omitnil,gte=1,lte=10"`
}

func (b *CatalogBiz) ListComment(ctx context.Context, params ListCommentParams) (commonmodel.PaginateResult[catalogmodel.Comment], error) {
	var zero commonmodel.PaginateResult[catalogmodel.Comment]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogComment(ctx, db.CountCatalogCommentParams{
		ID:        params.ID,
		AccountID: params.AccountID,
		RefType:   []db.CatalogCommentRefType{params.RefType},
		RefID:     params.RefID,
		ScoreFrom: pgutil.NullInt32ToPgInt4(params.ScoreFrom),
		ScoreTo:   pgutil.NullInt32ToPgInt4(params.ScoreTo),
	})
	if err != nil {
		return zero, err
	}

	dbComments, err := b.storage.ListCatalogComment(ctx, db.ListCatalogCommentParams{
		Limit:     pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:    pgutil.Int32ToPgInt4(params.Offset()),
		ID:        params.ID,
		AccountID: params.AccountID,
		RefType:   []db.CatalogCommentRefType{params.RefType},
		RefID:     params.RefID,
		ScoreFrom: pgutil.NullInt32ToPgInt4(params.ScoreFrom),
		ScoreTo:   pgutil.NullInt32ToPgInt4(params.ScoreTo),
	})
	if err != nil {
		return zero, err
	}
	var commentIDs []int64
	var accountIDs []int64
	for _, row := range dbComments {
		commentIDs = append(commentIDs, row.ID)
		accountIDs = append(accountIDs, row.AccountID)
	}

	// Map accounts to comments
	dbProfiles, err := b.storage.ListAccountProfile(ctx, db.ListAccountProfileParams{
		ID: accountIDs,
	})
	if err != nil {
		return zero, err
	}
	// map[accountID]db.AccountProfile
	profileMap := lo.KeyBy(dbProfiles, func(a db.AccountProfile) int64 { return a.ID })

	// Map avatar accounts
	avatars, err := b.storage.ListCommonResource(ctx, db.ListCommonResourceParams{
		ID: lo.Map(dbProfiles, func(a db.AccountProfile, _ int) pgtype.UUID { return a.AvatarRsID }),
	})
	if err != nil {
		return zero, err
	}
	avatarMap := lo.KeyBy(avatars, func(r db.CommonResource) pgtype.UUID { return r.ID })

	// Map resources to comments
	dbResources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.CommonResourceRefTypeComment,
		RefID:   commentIDs,
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]commonmodel.Resource)
	for _, row := range dbResources {
		// url, err :=

		resourceMap[row.RefID] = append(resourceMap[row.RefID], commonmodel.Resource{
			ID:       row.ID.Bytes,
			Url:      b.common.MustGetFileURL(ctx, row.Provider, row.ObjectKey),
			Mime:     row.Mime,
			Size:     row.Size,
			Checksum: pgutil.PgTextToNullString(row.Checksum),
		})
	}

	var comments []catalogmodel.Comment
	for _, row := range dbComments {
		profile := profileMap[row.AccountID]
		name := "Unknown User"
		if profile.Name.Valid {
			name = profile.Name.String
		}
		var avatar *commonmodel.Resource
		if profile.AvatarRsID.Valid {
			a := avatarMap[profile.AvatarRsID]
			avatar = &commonmodel.Resource{
				ID:   a.ID.Bytes,
				Url:  b.common.MustGetFileURL(ctx, a.Provider, a.ObjectKey),
				Mime: a.Mime,
				Size: a.Size,
			}
		}

		comments = append(comments, catalogmodel.Comment{
			ID: row.ID,
			Account: catalogmodel.CommentAccount{
				ID:       row.AccountID,
				Name:     name,
				Avatar:   avatar,
				Verified: profile.PhoneVerified || profile.EmailVerified,
			},
			Body:        row.Body,
			Upvote:      row.Upvote,
			Downvote:    row.Downvote,
			Score:       row.Score,
			DateCreated: row.DateCreated,
			DateUpdated: row.DateUpdated,
			Resources:   slice.EnsureSlice(resourceMap[row.ID]),
		})
	}

	return commonmodel.PaginateResult[catalogmodel.Comment]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       comments,
	}, nil
}

type CreateCommentParams struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount

	RefType db.CatalogCommentRefType `validate:"required"`
	RefID   int64                    `validate:"required,gt=0"`
	Body    string                   `validate:"required,min=1,max=1000"`
	Score   int32                    `validate:"required,gte=1,lte=10"`

	ResourceIDs []uuid.UUID `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateComment(ctx context.Context, params CreateCommentParams) (catalogmodel.Comment, error) {
	var zero catalogmodel.Comment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		comment   db.CatalogComment
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		comment, err = txStorage.CreateDefaultCatalogComment(ctx, db.CreateDefaultCatalogCommentParams{
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
			Storage:        txStorage,
			Account:        params.Account,
			RefType:        db.CommonResourceRefTypeComment,
			RefID:          comment.ID,
			ResourceIDs:    params.ResourceIDs,
			EmptyResources: true,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create comment: %w", err)
	}

	return catalogmodel.Comment{
		ID:          comment.ID,
		Account:     catalogmodel.CommentAccount{ID: params.Account.ID},
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
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount

	ID            int64       `validate:"required,gt=0"`
	Body          null.String `validate:"omitempty,min=1,max=1000"`
	Score         null.Int32  `validate:"omitempty,gte=1,lte=10"`
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
		comment   db.CatalogComment
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error

		// Update base comment info
		comment, err = txStorage.UpdateCatalogComment(ctx, db.UpdateCatalogCommentParams{
			ID:    params.ID,
			Body:  pgutil.NullStringToPgText(params.Body),
			Score: pgutil.NullInt32ToPgInt4(params.Score),
		})
		if err != nil {
			return err
		}

		// Update upvote/downvote count
		if params.UpvoteDelta.Valid || params.DownvoteDelta.Valid {
			if err := txStorage.UpdateCatalogCommentUpvoteDownvote(ctx, db.UpdateCatalogCommentUpvoteDownvoteParams{
				ID:            params.ID,
				UpvoteDelta:   pgutil.NullInt64ToPgInt8(params.UpvoteDelta),
				DownvoteDelta: pgutil.NullInt64ToPgInt8(params.DownvoteDelta),
			}); err != nil {
				return err
			}
		}

		// Update resources
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         txStorage,
			Account:         params.Account,
			RefType:         db.CommonResourceRefTypeComment,
			RefID:           params.ID,
			ResourceIDs:     params.ResourceIDs,
			EmptyResources:  params.EmptyResources,
			DeleteResources: true,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update comment: %w", err)
	}

	return catalogmodel.Comment{
		ID:          comment.ID,
		Account:     catalogmodel.CommentAccount{ID: params.Account.ID},
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
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount

	CommentIDs []int64 `validate:"required,dive,gt=0"`
}

func (b *CatalogBiz) DeleteComment(ctx context.Context, params DeleteCommentParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		// Delete base comments
		if err := txStorage.DeleteCatalogComment(ctx, db.DeleteCatalogCommentParams{
			ID: params.CommentIDs,
		}); err != nil {
			return err
		}

		// Remove associated resources
		// if err := b.shared.DeleteResources(ctx, txStorage, commonbiz.DeleteResourcesParams{
		// 	RefType:             db.CommonResourceRefTypeComment,
		// 	RefID:               params.CommentIDs,
		// 	DeleteResources:     true,
		// 	SkipDeleteResources: nil,
		// }); err != nil {
		// 	return err
		// }
		// TODO: now: use update resources instead
		// TODO: remove resources that are no longer referenced by any

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	return nil
}

package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
)

type ListCommentParams struct {
	sharedmodel.PaginationParams
	RefType   db.CatalogCommentRefType `validate:"required"`
	ID        []int64                  `validate:"omitempty,dive,gt=0"`
	AccountID []int64                  `validate:"omitempty,dive,gt=0"`
	RefID     []int64                  `validate:"omitempty,dive,gt=0"`
	ScoreFrom null.Int32               `validate:"omitnil,gte=1,lte=10"`
	ScoreTo   null.Int32               `validate:"omitnil,gte=1,lte=10"`
}

func (b *CatalogBiz) ListComment(ctx context.Context, params ListCommentParams) (sharedmodel.PaginateResult[catalogmodel.Comment], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.Comment]

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
	profileMap := slice.NewMap(dbProfiles, func(a db.AccountProfile) int64 { return a.ID })

	// Map avatar accounts
	avatars, err := b.storage.ListSharedResource(ctx, db.ListSharedResourceParams{
		ID: slice.Map(dbProfiles, func(a db.AccountProfile) int64 { return a.AvatarRsID.Int64 }),
	})
	if err != nil {
		return zero, err
	}
	avatarMap := slice.NewMap(avatars, func(r db.SharedResource) int64 { return r.ID })

	// Map resources to comments
	dbResources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.SharedResourceRefTypeComment,
		RefID:   commentIDs,
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]sharedmodel.Resource)
	for _, row := range dbResources {
		// url, err :=

		resourceMap[row.RefID] = append(resourceMap[row.RefID], sharedmodel.Resource{
			ID:   row.ID,
			Url:  sharedbiz.GetResourceURL(row.Code),
			Mime: row.Mime,

			FileSize: pgutil.PgInt8ToNullInt64(row.FileSize),
			Width:    pgutil.PgInt4ToNullInt32(row.Width),
			Height:   pgutil.PgInt4ToNullInt32(row.Height),
			Duration: pgutil.PgFloat8ToNullFloat(row.Duration),
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
		var avatar *sharedmodel.Resource
		if profile.AvatarRsID.Valid {
			a := avatarMap[profile.AvatarRsID.Int64]
			avatar = &sharedmodel.Resource{
				ID:       a.ID,
				Url:      sharedbiz.GetResourceURL(a.Code),
				Mime:     a.Mime,
				FileSize: pgutil.PgInt8ToNullInt64(a.FileSize),
				Width:    pgutil.PgInt4ToNullInt32(a.Width),
				Height:   pgutil.PgInt4ToNullInt32(a.Height),
				Duration: pgutil.PgFloat8ToNullFloat(a.Duration),
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
			Resources:   slice.NonNil(resourceMap[row.ID]),
		})
	}

	return sharedmodel.PaginateResult[catalogmodel.Comment]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       comments,
	}, nil
}

type CreateCommentParams struct {
	Account authmodel.AuthenticatedAccount

	RefType db.CatalogCommentRefType `validate:"required"`
	RefID   int64                    `validate:"required,gt=0"`
	Body    string                   `validate:"required,min=1,max=1000"`
	Score   int32                    `validate:"required,gte=1,lte=10"`

	Resources []sharedmodel.CreateResource `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateComment(ctx context.Context, params CreateCommentParams) (db.CatalogComment, error) {
	var zero db.CatalogComment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	comment, err := txStorage.CreateDefaultCatalogComment(ctx, db.CreateDefaultCatalogCommentParams{
		AccountID: params.Account.ID,
		RefType:   params.RefType,
		RefID:     params.RefID,
		Body:      params.Body,
		Score:     params.Score,
	})
	if err != nil {
		return zero, err
	}

	// Attach resources
	if len(params.Resources) > 0 {
		var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

		resources, err := txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
			ID:         slice.Map(params.Resources, func(r sharedmodel.CreateResource) int64 { return r.FileID }),
			UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
		})
		if err != nil {
			return zero, err
		}
		if len(resources) != len(params.Resources) {
			// Some resources not found or not belong to the user
			return zero, sharedmodel.ErrResourceNotFound
		}

		for order, res := range params.Resources {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
				RsID:      res.FileID,
				RefType:   db.SharedResourceRefTypeComment,
				RefID:     comment.ID,
				Order:     int32(order),
				IsPrimary: false,
			})

			if _, err := txStorage.CreateCopyDefaultSharedResourceReference(ctx, createResourceArgs); err != nil {
				return zero, err
			}
		}
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return comment, nil
}

type UpdateCommentParams struct {
	Account authmodel.AuthenticatedAccount

	ID            int64
	Body          null.String `validate:"omitempty,min=1,max=1000"`
	Score         null.Int32  `validate:"omitempty,gte=1,lte=10"`
	UpvoteDelta   null.Int64  `validate:"omitempty,ne=0"`
	DownvoteDelta null.Int64  `validate:"omitempty,ne=0"`

	Resources      []sharedmodel.CreateResource `validate:"omitempty,dive"`
	EmptyResources bool
}

func (b *CatalogBiz) UpdateComment(ctx context.Context, params UpdateCommentParams) (db.CatalogComment, error) {
	var zero db.CatalogComment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// Update base comment info
	comment, err := txStorage.UpdateCatalogComment(ctx, db.UpdateCatalogCommentParams{
		ID:    params.ID,
		Body:  pgutil.NullStringToPgText(params.Body),
		Score: pgutil.NullInt32ToPgInt4(params.Score),
	})
	if err != nil {
		return zero, err
	}

	// Update upvote/downvote count
	if params.UpvoteDelta.Valid || params.DownvoteDelta.Valid {
		if err := txStorage.UpdateCatalogCommentUpvoteDownvote(ctx, db.UpdateCatalogCommentUpvoteDownvoteParams{
			ID:            params.ID,
			UpvoteDelta:   pgutil.NullInt64ToPgInt8(params.UpvoteDelta),
			DownvoteDelta: pgutil.NullInt64ToPgInt8(params.DownvoteDelta),
		}); err != nil {
			return zero, err
		}
	}

	// Update resources
	if len(params.Resources) > 0 || params.EmptyResources {
		// Delete old resources
		if err := txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
			RefType: []db.SharedResourceRefType{db.SharedResourceRefTypeComment},
			RefID:   []int64{params.ID},
		}); err != nil {
			return zero, err
		}

		// Attach resources

		var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

		resources, err := txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
			ID:         slice.Map(params.Resources, func(r sharedmodel.CreateResource) int64 { return r.FileID }),
			UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
		})
		if err != nil {
			return zero, err
		}
		if len(resources) != len(params.Resources) {
			// Some resources not found or not belong to the user
			return zero, sharedmodel.ErrResourceNotFound
		}

		for order, res := range params.Resources {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
				RsID:      res.FileID,
				RefType:   db.SharedResourceRefTypeComment,
				RefID:     comment.ID,
				Order:     int32(order),
				IsPrimary: false,
			})

			if _, err := txStorage.CreateCopyDefaultSharedResourceReference(ctx, createResourceArgs); err != nil {
				return zero, err
			}
		}
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return comment, nil
}

type DeleteCommentParams struct {
	Account authmodel.AuthenticatedAccount

	IDs []int64 `validate:"required,dive,gt=0"`
}

func (b *CatalogBiz) DeleteComment(ctx context.Context, params DeleteCommentParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	// Delete base comments
	if err := txStorage.DeleteCatalogComment(ctx, db.DeleteCatalogCommentParams{
		ID: params.IDs,
	}); err != nil {
		return err
	}

	// Remove associated resources
	if err = txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
		RefType: []db.SharedResourceRefType{db.SharedResourceRefTypeComment},
		RefID:   params.IDs,
	}); err != nil {
		return err
	}

	// TODO: remove resources that are no longer referenced by any

	return txStorage.Commit(ctx)
}

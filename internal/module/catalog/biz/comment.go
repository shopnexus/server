package catalogbiz

import (
	"context"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

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
		Offset:    pgutil.Int32ToPgInt4(params.GetOffset()),
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
	for _, row := range dbComments {
		commentIDs = append(commentIDs, row.ID)
	}

	dbResources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.SharedResourceRefTypeComment,
		RefID:   commentIDs,
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]sharedmodel.Resource)
	for _, row := range dbResources {
		resourceMap[row.RefID] = append(resourceMap[row.RefID], sharedmodel.Resource{
			ID:       row.ID,
			Mime:     row.Mime,
			Url:      row.Url,
			FileSize: pgutil.PgInt8ToNullInt64(row.FileSize),
			Width:    pgutil.PgInt4ToNullInt32(row.Width),
			Height:   pgutil.PgInt4ToNullInt32(row.Height),
			Duration: pgutil.PgFloat8ToNullFloat(row.Duration),
		})
	}

	var comments []catalogmodel.Comment
	for _, row := range dbComments {
		comments = append(comments, catalogmodel.Comment{
			CatalogComment: row,
			Resources:      resourceMap[row.ID],
		})
	}

	return sharedmodel.PaginateResult[catalogmodel.Comment]{
		Data:       comments,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
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

	var createResourceArgs []db.CreateCopyDefaultSharedResourceParams
	for _, res := range params.Resources {
		createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceParams{
			Mime: "image/jpeg", // TODO: support other mime types
			//OwnerType: db.SharedResourceTypeProductSpu,
			//OwnerID:   comment.ID,
			//Order:     res.Order,
			Url:        res.Url,
			UploadedBy: pgtype.Int8{Int64: params.Account.ID, Valid: true},
		})
	}
	if len(createResourceArgs) > 0 {
		if _, err := txStorage.CreateCopyDefaultSharedResource(ctx, createResourceArgs); err != nil {
			return zero, err
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
	if len(params.Resources) > 0 {
		// Delete old resources
		if err := txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
			RefType: []db.SharedResourceRefType{db.SharedResourceRefTypeComment},
			RefID:   []int64{params.ID},
		}); err != nil {
			return zero, err
		}

		// Add new resources
		var createResourceArgs []db.CreateCopyDefaultSharedResourceParams
		for _, res := range params.Resources {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceParams{
				Mime: "image/jpeg", // TODO: support other mime types
				Url:  res.Url,
				//FileSize:   pgtype.Int8{},
				//Width:      pgtype.Int4{},
				//Height:     pgtype.Int4{},
				//Duration:   pgtype.Float8{},
				//Checksum:   pgtype.Text{},
				UploadedBy: pgtype.Int8{Int64: params.Account.ID, Valid: true},
			})
		}
		if len(createResourceArgs) > 0 {
			if _, err := txStorage.CreateCopyDefaultSharedResource(ctx, createResourceArgs); err != nil {
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

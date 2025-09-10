package catalogbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
)

type ListCommentParams struct {
	sharedmodel.PaginationParams
	RefType   db.CatalogCommentRefType
	ID        []int64
	AccountID []int64
	RefID     []int64
	ScoreFrom *int32
	ScoreTo   *int32
}

func (b *CatalogBiz) ListComment(ctx context.Context, params ListCommentParams) (sharedmodel.PaginateResult[catalogmodel.Comment], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.Comment]

	total, err := b.storage.CountCatalogComment(ctx, db.CountCatalogCommentParams{
		ID:        params.ID,
		AccountID: params.AccountID,
		RefType:   []db.CatalogCommentRefType{params.RefType},
		RefID:     params.RefID,
		ScoreFrom: pgutil.PtrToPgtype(params.ScoreFrom, pgutil.Int32ToPgInt4),
		ScoreTo:   pgutil.PtrToPgtype(params.ScoreTo, pgutil.Int32ToPgInt4),
	})
	if err != nil {
		return zero, err
	}

	commentRows, err := b.storage.ListCatalogComment(ctx, db.ListCatalogCommentParams{
		Limit:     pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:    pgutil.Int32ToPgInt4(params.GetOffset()),
		ID:        params.ID,
		AccountID: params.AccountID,
		RefType:   []db.CatalogCommentRefType{params.RefType},
		RefID:     params.RefID,
		ScoreFrom: pgutil.PtrToPgtype(params.ScoreFrom, pgutil.Int32ToPgInt4),
		ScoreTo:   pgutil.PtrToPgtype(params.ScoreTo, pgutil.Int32ToPgInt4),
	})
	if err != nil {
		return zero, err
	}
	var commentIDs []int64
	for _, row := range commentRows {
		commentIDs = append(commentIDs, row.ID)
	}

	resourceRows, err := b.storage.ListSharedResource(ctx, db.ListSharedResourceParams{
		OwnerType: []db.SharedResourceType{db.SharedResourceTypeProductSpu},
		OwnerID:   commentIDs,
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]catalogmodel.Resource)
	for _, row := range resourceRows {
		resourceMap[row.OwnerID] = append(resourceMap[row.OwnerID], catalogmodel.Resource{
			Order:    row.Order,
			Url:      row.Url,
			MimeType: row.MimeType,
		})
	}

	var comments []catalogmodel.Comment
	for _, row := range commentRows {
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

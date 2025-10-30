package sharedbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

func (b *SharedBiz) GetResourceURLByID(ctx context.Context, resourceID int64) null.String {
	if resourceID == 0 {
		return null.String{}
	}

	rs, err := b.storage.GetSharedResource(ctx, db.GetSharedResourceParams{
		ID: pgutil.Int64ToPgInt8(resourceID),
	})
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(b.MustGetFileURL(ctx, rs.Provider, rs.ObjectKey))
}
